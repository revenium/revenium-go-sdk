package anthropic

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	anthropicsdk "github.com/anthropics/anthropic-sdk-go"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	brtypes "github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
	"github.com/revenium/revenium-go-sdk/core/metering"
	"github.com/revenium/revenium-go-sdk/core/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func messagePayload(resp *anthropicsdk.Message, provider string) *metering.MeteringPayload {
	return buildTurnPayload(messageTurnUsage(resp), turnTiming{start: time.Now(), duration: time.Second}, provider, false, nil)
}

const oneHourWriteMessage = `{"id":"msg_1h","type":"message","role":"assistant","model":"claude-haiku-4-5","content":[{"type":"text","text":"Hi"}],"stop_reason":"end_turn","usage":{"input_tokens":14,"cache_creation_input_tokens":12603,"cache_read_input_tokens":0,"cache_creation":{"ephemeral_5m_input_tokens":0,"ephemeral_1h_input_tokens":12603},"output_tokens":7}}`

const fiveMinuteWriteMessage = `{"id":"msg_5m","type":"message","role":"assistant","model":"claude-haiku-4-5","content":[{"type":"text","text":"Hi"}],"stop_reason":"end_turn","usage":{"input_tokens":14,"cache_creation_input_tokens":4200,"cache_read_input_tokens":0,"cache_creation":{"ephemeral_5m_input_tokens":4200,"ephemeral_1h_input_tokens":0},"output_tokens":7}}`

const messageWithoutTTLSplit = `{"id":"msg_flat","type":"message","role":"assistant","model":"claude-haiku-4-5","content":[{"type":"text","text":"Hi"}],"stop_reason":"end_turn","usage":{"input_tokens":14,"cache_creation_input_tokens":4200,"cache_read_input_tokens":0,"output_tokens":7}}`

const oneHourMessageStartEvent = `{"type":"message_start","message":{"model":"claude-haiku-4-5-20251001","id":"msg_stream_1h","type":"message","role":"assistant","content":[],"stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":9,"cache_creation_input_tokens":8404,"cache_read_input_tokens":0,"cache_creation":{"ephemeral_5m_input_tokens":0,"ephemeral_1h_input_tokens":8404},"output_tokens":3,"service_tier":"standard"}}}`

const oneHourMessageDeltaEvent = `{"type":"message_delta","delta":{"stop_reason":"max_tokens","stop_sequence":null},"usage":{"input_tokens":9,"cache_creation_input_tokens":8404,"cache_read_input_tokens":0,"output_tokens":5}}`

func decodeMessage(t *testing.T, raw string) *anthropicsdk.Message {
	t.Helper()
	var msg anthropicsdk.Message
	require.NoError(t, json.Unmarshal([]byte(raw), &msg))
	return &msg
}

func decodeStreamEvent(t *testing.T, raw string) anthropicsdk.MessageStreamEventUnion {
	t.Helper()
	var event anthropicsdk.MessageStreamEventUnion
	require.NoError(t, json.Unmarshal([]byte(raw), &event))
	return event
}

func meteredStreamPayload(t *testing.T, configure func(r *ReveniumAnthropic) *StreamingWrapper) map[string]interface{} {
	t.Helper()
	mock := testutil.NewMockMeteringServer()
	t.Cleanup(mock.Close)
	r := newTestAnthropic(t, mock.URL())
	t.Cleanup(func() { _ = r.Close() })

	require.NoError(t, configure(r).Close())
	r.Flush()

	require.True(t, mock.WaitForPayloads(1, time.Second))
	payloads := mock.GetPayloads()
	require.Len(t, payloads, 1)
	return payloads[0]
}

func streamingWrapperFor(r *ReveniumAnthropic, stream interface{}) *StreamingWrapper {
	return newStreamingWrapper(stream, r.config, map[string]interface{}{}, &MessagesInterface{parent: r}, "claude-haiku-4-5", "Anthropic", false, time.Now())
}

func TestMessagePayload_SendsOneHourCacheWrites(t *testing.T) {
	payload := messagePayload(decodeMessage(t, oneHourWriteMessage), "Anthropic")

	assert.Equal(t, int64(12603), payload.CacheCreationTokenCount)
	require.NotNil(t, payload.CacheCreation1hTokenCount)
	assert.Equal(t, int64(12603), *payload.CacheCreation1hTokenCount)
	require.NotNil(t, payload.CacheCreation5mTokenCount)
	assert.Equal(t, int64(0), *payload.CacheCreation5mTokenCount)
}

func TestMessagePayload_FiveMinuteWritesKeepTheirAggregate(t *testing.T) {
	payload := messagePayload(decodeMessage(t, fiveMinuteWriteMessage), "Anthropic")

	assert.Equal(t, int64(4200), payload.CacheCreationTokenCount)
	require.NotNil(t, payload.CacheCreation5mTokenCount)
	assert.Equal(t, int64(4200), *payload.CacheCreation5mTokenCount)
	require.NotNil(t, payload.CacheCreation1hTokenCount)
	assert.Equal(t, int64(0), *payload.CacheCreation1hTokenCount)
}

func TestMessagePayload_OmitsSplitTheResponseDidNotReport(t *testing.T) {
	payload := messagePayload(decodeMessage(t, messageWithoutTTLSplit), "Anthropic")

	assert.Equal(t, int64(4200), payload.CacheCreationTokenCount)
	assert.Nil(t, payload.CacheCreation5mTokenCount)
	assert.Nil(t, payload.CacheCreation1hTokenCount)
}

func TestStreamingWrapper_SendsOneHourCacheWritesFromMessageStart(t *testing.T) {
	p := meteredStreamPayload(t, func(r *ReveniumAnthropic) *StreamingWrapper {
		w := streamingWrapperFor(r, nil)
		w.processTypedEvent(decodeStreamEvent(t, oneHourMessageStartEvent))
		w.processTypedEvent(decodeStreamEvent(t, oneHourMessageDeltaEvent))
		return w
	})

	assert.Equal(t, float64(8404), p["cacheCreationTokenCount"])
	assert.Equal(t, float64(8404), p["cacheCreation1hTokenCount"])
	assert.Equal(t, float64(0), p["cacheCreation5mTokenCount"])
}

const zeroUncachedInputMessageStartEvent = `{"type":"message_start","message":{"model":"claude-haiku-4-5-20251001","id":"msg_stream_zero","type":"message","role":"assistant","content":[],"stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":0,"cache_creation_input_tokens":8404,"cache_read_input_tokens":0,"cache_creation":{"ephemeral_5m_input_tokens":0,"ephemeral_1h_input_tokens":8404},"output_tokens":1,"service_tier":"standard"}}}`

const zeroUncachedInputMessageDeltaEvent = `{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"input_tokens":0,"cache_creation_input_tokens":8404,"cache_read_input_tokens":0,"output_tokens":5}}`

func TestStreamingWrapper_KeepsOneHourWritesWhenNoInputIsUncached(t *testing.T) {
	p := meteredStreamPayload(t, func(r *ReveniumAnthropic) *StreamingWrapper {
		w := streamingWrapperFor(r, nil)
		w.processTypedEvent(decodeStreamEvent(t, zeroUncachedInputMessageStartEvent))
		w.processTypedEvent(decodeStreamEvent(t, zeroUncachedInputMessageDeltaEvent))
		return w
	})

	assert.Equal(t, float64(8404), p["cacheCreationTokenCount"])
	assert.Equal(t, float64(8404), p["cacheCreation1hTokenCount"])
	assert.Equal(t, float64(0), p["cacheCreation5mTokenCount"])
}

func TestStreamingWrapper_OmitsSplitWhenStreamReportedNone(t *testing.T) {
	p := meteredStreamPayload(t, func(r *ReveniumAnthropic) *StreamingWrapper {
		w := streamingWrapperFor(r, nil)
		w.processTypedEvent(decodeStreamEvent(t, oneHourMessageDeltaEvent))
		return w
	})

	assert.Equal(t, float64(8404), p["cacheCreationTokenCount"])
	assert.NotContains(t, p, "cacheCreation1hTokenCount")
	assert.NotContains(t, p, "cacheCreation5mTokenCount")
}

func TestCreateMessage_InvokeModelSendsOneHourCacheWrites(t *testing.T) {
	mock := &mockBedrockClient{
		invokeModelResp: &bedrockruntime.InvokeModelOutput{Body: []byte(oneHourWriteMessage)},
	}
	adapter := &BedrockAdapter{config: &Config{}, client: mock}

	resp, err := adapter.CreateMessage(context.Background(), makeTestParams("claude-haiku-4-5", "Hello"))
	require.NoError(t, err)

	payload := messagePayload(resp, "AWS")
	assert.Equal(t, int64(12603), payload.CacheCreationTokenCount)
	require.NotNil(t, payload.CacheCreation1hTokenCount)
	assert.Equal(t, int64(12603), *payload.CacheCreation1hTokenCount)
	require.NotNil(t, payload.CacheCreation5mTokenCount)
	assert.Equal(t, int64(0), *payload.CacheCreation5mTokenCount)
}

func TestCreateMessage_InvokeModelWithoutSplitBehavesAsBefore(t *testing.T) {
	mock := &mockBedrockClient{
		invokeModelResp: &bedrockruntime.InvokeModelOutput{Body: []byte(messageWithoutTTLSplit)},
	}
	adapter := &BedrockAdapter{config: &Config{}, client: mock}

	resp, err := adapter.CreateMessage(context.Background(), makeTestParams("claude-haiku-4-5", "Hello"))
	require.NoError(t, err)

	payload := messagePayload(resp, "AWS")
	assert.Equal(t, int64(4200), payload.CacheCreationTokenCount)
	assert.Nil(t, payload.CacheCreation5mTokenCount)
	assert.Nil(t, payload.CacheCreation1hTokenCount)
}

func TestCreateMessageConverse_SendsNoSplitBecauseConverseReportsNone(t *testing.T) {
	mock := &mockBedrockClient{
		converseResp: &bedrockruntime.ConverseOutput{
			StopReason: brtypes.StopReasonEndTurn,
			Usage: &brtypes.TokenUsage{
				InputTokens:           aws.Int32(10),
				OutputTokens:          aws.Int32(5),
				TotalTokens:           aws.Int32(15),
				CacheWriteInputTokens: aws.Int32(4200),
			},
		},
	}
	adapter := &BedrockAdapter{config: &Config{}, client: mock}

	resp, err := adapter.CreateMessageConverse(context.Background(), makeTestParams("claude-haiku-4-5", "Hello"))
	require.NoError(t, err)

	payload := messagePayload(resp, "AWS")
	assert.Equal(t, int64(4200), payload.CacheCreationTokenCount)
	assert.Nil(t, payload.CacheCreation5mTokenCount)
	assert.Nil(t, payload.CacheCreation1hTokenCount)
}

func bedrockStreamOf(chunks ...string) *BedrockStreamingWrapper {
	events := make(chan brtypes.ResponseStream, len(chunks))
	for _, chunk := range chunks {
		events <- &brtypes.ResponseStreamMemberChunk{Value: brtypes.PayloadPart{Bytes: []byte(chunk)}}
	}
	close(events)
	stream := &BedrockStreamingWrapper{events: events, modelID: "anthropic.claude-haiku-4-5"}
	for stream.Next() {
	}
	return stream
}

func TestStreamingWrapper_BedrockStreamSendsOneHourCacheWrites(t *testing.T) {
	stream := bedrockStreamOf(oneHourMessageStartEvent, oneHourMessageDeltaEvent)

	p := meteredStreamPayload(t, func(r *ReveniumAnthropic) *StreamingWrapper {
		return streamingWrapperFor(r, stream)
	})

	assert.Equal(t, float64(8404), p["cacheCreationTokenCount"])
	assert.Equal(t, float64(8404), p["cacheCreation1hTokenCount"])
	assert.Equal(t, float64(0), p["cacheCreation5mTokenCount"])
}

func TestStreamingWrapper_BedrockStreamWithoutSplitBehavesAsBefore(t *testing.T) {
	stream := bedrockStreamOf(oneHourMessageDeltaEvent)

	p := meteredStreamPayload(t, func(r *ReveniumAnthropic) *StreamingWrapper {
		return streamingWrapperFor(r, stream)
	})

	assert.Equal(t, float64(8404), p["cacheCreationTokenCount"])
	assert.NotContains(t, p, "cacheCreation1hTokenCount")
	assert.NotContains(t, p, "cacheCreation5mTokenCount")
}

func TestCacheWriteSplitOf_IgnoresBucketsThatAreNotNumbers(t *testing.T) {
	byTTL := decodeCacheCreation(map[string]interface{}{
		"cache_creation": map[string]interface{}{
			"ephemeral_5m_input_tokens": "lots",
			"ephemeral_1h_input_tokens": 300.0,
		},
	})

	split := cacheWriteSplitOf(byTTL)
	assert.Nil(t, split.fiveMinute)
	require.NotNil(t, split.oneHour)
	assert.Equal(t, int64(300), *split.oneHour)
}
