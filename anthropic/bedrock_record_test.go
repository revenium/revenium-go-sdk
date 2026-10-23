package anthropic

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/revenium/revenium-go-sdk/core/enforcement"
	"github.com/revenium/revenium-go-sdk/core/testutil"
)

const (
	streamedModelID = "us.anthropic.claude-haiku-4-5-20251001-v1:0"

	invokeStreamMessageStartWithCache = `{"type":"message_start","message":{"id":"msg_bdrk_01","type":"message","role":"assistant",` +
		`"model":"claude-haiku-4-5-20251001","content":[],"stop_reason":null,"stop_sequence":null,` +
		`"usage":{"input_tokens":1,"cache_creation_input_tokens":1139,"cache_read_input_tokens":421714,"output_tokens":2}}}`
	invokeStreamMessageStartWithoutCache = `{"type":"message_start","message":{"id":"msg_bdrk_01","type":"message","role":"assistant",` +
		`"model":"claude-haiku-4-5-20251001","content":[],"stop_reason":null,"stop_sequence":null,` +
		`"usage":{"input_tokens":1,"output_tokens":2}}}`
	invokeStreamBlockStart = `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`
	invokeStreamBlockDelta = `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}`
	invokeStreamBlockStop  = `{"type":"content_block_stop","index":0}`
	invokeStreamOutputOnly = `{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"output_tokens":162}}`
	invokeStreamFullUsage  = `{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},` +
		`"usage":{"input_tokens":1,"cache_creation_input_tokens":1139,"cache_read_input_tokens":421714,"output_tokens":162}}`
	invokeStreamStopWithCache = `{"type":"message_stop","amazon-bedrock-invocationMetrics":{"inputTokenCount":1,"outputTokenCount":162,` +
		`"invocationLatency":2112,"firstByteLatency":412,"cacheReadInputTokenCount":421714,"cacheWriteInputTokenCount":1139}}`
	invokeStreamStopWithoutCache = `{"type":"message_stop","amazon-bedrock-invocationMetrics":{"inputTokenCount":1,"outputTokenCount":162,` +
		`"invocationLatency":2112,"firstByteLatency":412}}`
)

func drainStream(t *testing.T, stream interface{}) {
	t.Helper()
	wrapper, ok := stream.(*StreamingWrapper)
	require.True(t, ok)
	for wrapper.Next() {
		wrapper.Current()
	}
	require.NoError(t, wrapper.Err())
	require.NoError(t, wrapper.Close())
}

func TestBedrockInvokeModelStreamRecordsCacheCountsWhereverBedrockReportsThem(t *testing.T) {
	layouts := map[string][]string{
		"cache counts only in message_start": {
			invokeStreamMessageStartWithCache, invokeStreamBlockStart, invokeStreamBlockDelta, invokeStreamBlockStop,
			invokeStreamOutputOnly, invokeStreamStopWithoutCache,
		},
		"cache counts only in the invocation metrics": {
			invokeStreamMessageStartWithoutCache, invokeStreamBlockStart, invokeStreamBlockDelta, invokeStreamBlockStop,
			invokeStreamOutputOnly, invokeStreamStopWithCache,
		},
		"cache counts in every usage block": {
			invokeStreamMessageStartWithCache, invokeStreamBlockStart, invokeStreamBlockDelta, invokeStreamBlockStop,
			invokeStreamFullUsage, invokeStreamStopWithCache,
		},
	}
	for name, layout := range layouts {
		t.Run(name, func(t *testing.T) {
			stub := newBedrockStub(t)
			stub.streamWith(invokeStreamChunks(layout...))
			mock := newMeteringMock(t)
			r := newBedrockAnthropic(t, bedrockConfig(mock.URL()))

			stream, err := r.Messages().CreateMessageStream(context.Background(), messageParams(streamedModelID))
			require.NoError(t, err)
			drainStream(t, stream)

			record := onlyRecord(t, r, mock)
			assert.Equal(t, 1.0, record["inputTokenCount"])
			assert.Equal(t, 162.0, record["outputTokenCount"])
			assert.Equal(t, 421714.0, record["cacheReadTokenCount"])
			assert.Equal(t, 1139.0, record["cacheCreationTokenCount"])
			assert.Equal(t, "END", record["stopReason"])
		})
	}
}

func converseStopEvents(stopReason string) []bedrockEvent {
	return []bedrockEvent{
		{eventType: "messageStart", payload: `{"role":"assistant"}`},
		{eventType: "contentBlockDelta", payload: `{"contentBlockIndex":0,"delta":{"text":"I can't help with that."}}`},
		{eventType: "contentBlockStop", payload: `{"contentBlockIndex":0}`},
		{eventType: "messageStop", payload: fmt.Sprintf(`{"stopReason":%q}`, stopReason)},
		{eventType: "metadata", payload: `{"usage":{"inputTokens":10,"outputTokens":6,"totalTokens":16},"metrics":{"latencyMs":5}}`},
	}
}

func converseAnswer(stopReason, usage string) string {
	return fmt.Sprintf(`{"output":{"message":{"role":"assistant","content":[{"text":"I can't help with that."}]}},`+
		`"stopReason":%q,"usage":%s,"metrics":{"latencyMs":5}}`, stopReason, usage)
}

func TestBedrockConverseRecordsGuardrailAndContentFilterStopsAsErrors(t *testing.T) {
	for _, stopReason := range []string{"guardrail_intervened", "content_filtered"} {
		t.Run(stopReason, func(t *testing.T) {
			stub := newBedrockStub(t)
			stub.respondWith(converseAnswer(stopReason, `{"inputTokens":10,"outputTokens":6,"totalTokens":16}`))
			stub.streamWith(converseStopEvents(stopReason))
			mock := newMeteringMock(t)
			r := newConverseAnthropic(t, mock.URL())

			_, err := r.Messages().CreateMessage(context.Background(), messageParams("claude-haiku-4-5-20251001"))
			require.NoError(t, err)
			assert.Equal(t, "ERROR", onlyRecord(t, r, mock)["stopReason"])

			mock.Reset()
			stream, err := r.Messages().CreateMessageStream(context.Background(), messageParams("claude-haiku-4-5-20251001"))
			require.NoError(t, err)
			drainStream(t, stream)
			assert.Equal(t, "ERROR", onlyRecord(t, r, mock)["stopReason"])
		})
	}
}

func TestBedrockStopReasonsThatMeanTheAnswerFailedMapToError(t *testing.T) {
	for _, stopReason := range []string{"guardrail_intervened", "content_filtered", "malformed_model_output", "malformed_tool_use"} {
		assert.Equal(t, "ERROR", MapStopReasonToRevenium(stopReason), stopReason)
	}
}

func TestBedrockRecordsOneModelNameOnEveryPath(t *testing.T) {
	const account = "123456789012"
	cases := []struct {
		name     string
		model    string
		arnBase  string
		recorded string
	}{
		{name: "inference profile id", model: streamedModelID, recorded: streamedModelID},
		{name: "model with AWS_MODEL_ARN_ID", model: "claude-haiku-4-5-20251001", arnBase: "arn:aws:bedrock:us-east-1:" + account, recorded: streamedModelID},
		{name: "inference profile ARN", model: "arn:aws:bedrock:us-east-1:" + account + ":inference-profile/" + streamedModelID, recorded: streamedModelID},
		{name: "unversioned model without AWS_MODEL_ARN_ID", model: "claude-sonnet-4-6", recorded: "anthropic.claude-sonnet-4-6"},
		{
			name:     "application inference profile ARN",
			model:    "arn:aws:bedrock:us-east-1:" + account + ":application-inference-profile/a1b2c3",
			recorded: "application-inference-profile/a1b2c3",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var recorded []string
			for _, useConverse := range []bool{false, true} {
				stub := newBedrockStub(t)
				stub.respondWith(invokeModelAnswer)
				stub.streamWith(invokeStreamChunks(invokeStreamMessageStartWithoutCache, invokeStreamBlockDelta, invokeStreamOutputOnly, invokeStreamStopWithoutCache))
				if useConverse {
					stub.respondWith(converseAnswer("end_turn", `{"inputTokens":10,"outputTokens":6,"totalTokens":16}`))
					stub.streamWith(converseStopEvents("end_turn"))
				}
				mock := newMeteringMock(t)
				cfg := bedrockConfig(mock.URL())
				cfg.BedrockUseConverse = useConverse
				cfg.AWSModelARNBase = tc.arnBase
				r := newBedrockAnthropic(t, cfg)

				_, err := r.Messages().CreateMessage(context.Background(), messageParams(tc.model))
				require.NoError(t, err)
				recorded = append(recorded, onlyRecord(t, r, mock)["model"].(string))

				mock.Reset()
				stream, err := r.Messages().CreateMessageStream(context.Background(), messageParams(tc.model))
				require.NoError(t, err)
				drainStream(t, stream)
				recorded = append(recorded, onlyRecord(t, r, mock)["model"].(string))
			}
			assert.Equal(t, []string{tc.recorded, tc.recorded, tc.recorded, tc.recorded}, recorded)
			for _, model := range recorded {
				assert.False(t, strings.Contains(model, account), "model %q carries the AWS account id", model)
			}
		})
	}
}

func TestRecordedBedrockModelKeepsTheAccountIDOutOfARNsThatNameNoModel(t *testing.T) {
	assert.Equal(t, "application-inference-profile/a1b2c3",
		recordedBedrockModel("arn:aws:bedrock:us-east-1:123456789012:application-inference-profile/a1b2c3"))
	assert.Equal(t, "anthropic.claude-haiku-4-5-20251001-v1:0",
		recordedBedrockModel("arn:aws:bedrock:us-east-1::foundation-model/anthropic.claude-haiku-4-5-20251001-v1:0"))
}

func cacheWriteSplitIn(record map[string]interface{}) map[string]interface{} {
	split := map[string]interface{}{}
	for _, key := range []string{"cacheCreation5mTokenCount", "cacheCreation1hTokenCount"} {
		if count, present := record[key]; present {
			split[key] = count
		}
	}
	return split
}

func TestBedrockConverseSendsTheCacheWriteSplitBedrockReports(t *testing.T) {
	cases := []struct {
		name  string
		usage string
		split map[string]interface{}
	}{
		{
			name:  "both TTLs",
			usage: `{"inputTokens":10,"outputTokens":6,"totalTokens":416,"cacheWriteInputTokens":400,"cacheDetails":[{"ttl":"5m","inputTokens":100},{"ttl":"1h","inputTokens":300}]}`,
			split: map[string]interface{}{"cacheCreation5mTokenCount": 100.0, "cacheCreation1hTokenCount": 300.0},
		},
		{
			name:  "one TTL, the other left out",
			usage: `{"inputTokens":10,"outputTokens":6,"totalTokens":316,"cacheWriteInputTokens":300,"cacheDetails":[{"ttl":"1h","inputTokens":300}]}`,
			split: map[string]interface{}{"cacheCreation1hTokenCount": 300.0},
		},
		{
			name:  "a bucket reported as zero",
			usage: `{"inputTokens":10,"outputTokens":6,"totalTokens":316,"cacheWriteInputTokens":300,"cacheDetails":[{"ttl":"5m","inputTokens":0},{"ttl":"1h","inputTokens":300}]}`,
			split: map[string]interface{}{"cacheCreation5mTokenCount": 0.0, "cacheCreation1hTokenCount": 300.0},
		},
		{
			name:  "entries short of the whole write",
			usage: `{"inputTokens":10,"outputTokens":6,"totalTokens":416,"cacheWriteInputTokens":400,"cacheDetails":[{"ttl":"1h","inputTokens":300}]}`,
			split: map[string]interface{}{},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stub := newBedrockStub(t)
			stub.respondWith(converseAnswer("end_turn", tc.usage))
			events := converseStopEvents("end_turn")
			events[len(events)-1] = bedrockEvent{eventType: "metadata", payload: `{"usage":` + tc.usage + `,"metrics":{"latencyMs":5}}`}
			stub.streamWith(events)
			mock := newMeteringMock(t)
			r := newConverseAnthropic(t, mock.URL())

			_, err := r.Messages().CreateMessage(context.Background(), messageParams("claude-haiku-4-5-20251001"))
			require.NoError(t, err)
			synchronous := onlyRecord(t, r, mock)

			mock.Reset()
			stream, err := r.Messages().CreateMessageStream(context.Background(), messageParams("claude-haiku-4-5-20251001"))
			require.NoError(t, err)
			drainStream(t, stream)
			streamed := onlyRecord(t, r, mock)

			assert.Equal(t, tc.split, cacheWriteSplitIn(synchronous), "synchronous")
			assert.Equal(t, tc.split, cacheWriteSplitIn(streamed), "streamed")
			assert.Equal(t, synchronous["cacheCreationTokenCount"], streamed["cacheCreationTokenCount"])
		})
	}
}

func TestBedrockCostControlsCheckTheModelTheRecordCarries(t *testing.T) {
	enforcement.Stop()
	t.Cleanup(enforcement.Stop)
	stub := newBedrockStub(t)
	stub.respondWith(invokeModelAnswer)
	rules := testutil.NewMockRulesServer()
	t.Cleanup(rules.Close)
	rules.SetPayload(enforcementTestTeam, `{"rules": [{"ruleId": 9, "name": "haiku-on-bedrock", "metricType": "TOTAL_COST",
		"threshold": 50, "currentValue": 61, "breached": true, "shadowMode": false, "action": "BLOCK",
		"filters": [{"dimension": "MODEL", "operator": "IS", "value": "`+streamedModelID+`"}]}]}`)
	cfg := bedrockConfig(newMeteringMock(t).URL())
	cfg.AWSModelARNBase = "arn:aws:bedrock:us-east-1:123456789012"
	cfg.Revenium.EnforcementBaseURL = rules.URL()
	cfg.Revenium.TeamID = enforcementTestTeam
	r := newBedrockAnthropic(t, cfg)

	_, err := r.Messages().CreateMessage(context.Background(), messageParams("claude-haiku-4-5-20251001"))
	var blocked *enforcement.ErrCostLimitExceeded
	require.True(t, errors.As(err, &blocked), "the call recorded as %s should be blocked, got %v", streamedModelID, err)

	_, err = r.Messages().CreateMessageStream(context.Background(), messageParams("claude-haiku-4-5-20251001"))
	require.True(t, errors.As(err, &blocked), "the stream recorded as %s should be blocked, got %v", streamedModelID, err)
	assert.Empty(t, stub.received())
}

func TestBedrockStreamKeepsTheModelSetWithSetModel(t *testing.T) {
	stub := newBedrockStub(t)
	stub.streamWith(invokeStreamChunks(invokeStreamMessageStartWithoutCache, invokeStreamBlockDelta, invokeStreamOutputOnly, invokeStreamStopWithoutCache))
	mock := newMeteringMock(t)
	r := newBedrockAnthropic(t, bedrockConfig(mock.URL()))

	stream, err := r.Messages().CreateMessageStream(context.Background(), messageParams(streamedModelID))
	require.NoError(t, err)
	stream.(*StreamingWrapper).SetModel("customer-label")
	drainStream(t, stream)

	assert.Equal(t, "customer-label", onlyRecord(t, r, mock)["model"])
}
