package anthropic

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	anthropicsdk "github.com/anthropics/anthropic-sdk-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const rejectedRequestReply = `{"type":"error","error":{"type":"invalid_request_error","message":"prompt is too long: say hello please"}}`

func rejectEveryRequest(t *testing.T, status int, body string) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	t.Setenv("ANTHROPIC_BASE_URL", server.URL)
}

var failingSurfaces = []turnSurface{
	{"messages", false, func(t *testing.T, r *ReveniumAnthropic, ctx context.Context) {
		_, err := r.Messages().CreateMessage(ctx, messageParams("claude-haiku-4-5"))
		require.Error(t, err)
	}},
	{"messages stream", true, func(t *testing.T, r *ReveniumAnthropic, ctx context.Context) {
		stream, err := r.Messages().CreateMessageStream(ctx, messageParams("claude-haiku-4-5"))
		require.NoError(t, err)
		wrapper := stream.(*StreamingWrapper)
		for wrapper.Next() {
		}
		require.Error(t, wrapper.Err())
		_ = wrapper.Close()
	}},
	{"beta messages", false, func(t *testing.T, r *ReveniumAnthropic, ctx context.Context) {
		_, err := r.BetaMessages().CreateMessage(ctx, betaParams())
		require.Error(t, err)
	}},
	{"beta messages stream", true, func(t *testing.T, r *ReveniumAnthropic, ctx context.Context) {
		stream, err := r.BetaMessages().CreateMessageStream(ctx, betaParams())
		require.NoError(t, err)
		for stream.Next() {
		}
		require.Error(t, stream.Err())
		_ = stream.Close()
	}},
	{"tool runner", false, func(t *testing.T, r *ReveniumAnthropic, ctx context.Context) {
		runner := r.BetaMessages().NewToolRunner([]anthropicsdk.BetaTool{&lookupTool{}}, runnerParams())
		_, err := runner.RunToCompletion(ctx)
		require.Error(t, err)
	}},
	{"streaming tool runner", true, func(t *testing.T, r *ReveniumAnthropic, ctx context.Context) {
		runner := r.BetaMessages().NewToolRunnerStreaming([]anthropicsdk.BetaTool{&lookupTool{}}, runnerParams())
		var failed bool
		for _, err := range runner.NextStreaming(ctx) {
			failed = failed || err != nil
		}
		require.True(t, failed)
	}},
}

func TestFailedCall_IsRecordedOnceAsAnErrorOnEverySurface(t *testing.T) {
	for _, surface := range failingSurfaces {
		t.Run(surface.name, func(t *testing.T) {
			rejectEveryRequest(t, http.StatusBadRequest, rejectedRequestReply)
			r, mock := newMeteredAnthropic(t)

			surface.call(t, r, context.Background())

			record := flushedRecords(t, r, mock, 1)[0]
			assert.Equal(t, "ERROR", record["stopReason"])
			assert.Equal(t, "invalid_request_error (HTTP 400)", record["errorReason"])
			assert.Equal(t, float64(400), record["errorCode"])
			assert.Equal(t, surface.streamed, record["isStreamed"])
			assert.Equal(t, float64(0), record["inputTokenCount"], "a refused request is not billed")
			assert.Equal(t, "Anthropic", record["provider"], "the same provider name as its successful calls")
		})
	}
}

func TestToolRunnerTurnThatFailsBeforeAnyRequestIsNotRecorded(t *testing.T) {
	rejectEveryRequest(t, http.StatusBadRequest, rejectedRequestReply)
	r, mock := newMeteredAnthropic(t)
	params := runnerParams()
	params.Compaction = anthropicsdk.BetaCompactionConfigUnionParam{OfSummarize: &anthropicsdk.BetaSummarizeCompactionParam{}}

	runner := r.BetaMessages().NewToolRunner([]anthropicsdk.BetaTool{&lookupTool{}}, params)
	_, err := runner.NextMessage(context.Background())
	require.Error(t, err)

	r.Flush()
	assert.Empty(t, mock.GetPayloads())
}

func TestStreamThatBreaksOffKeepsTheTokensAnthropicReportedAndIsAnError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"claude-haiku-4-5\",\"content\":[],\"stop_reason\":null,\"usage\":{\"input_tokens\":25,\"output_tokens\":1}}}\n\n"))
		_, _ = w.Write([]byte("event: error\ndata: {\"type\":\"error\",\"error\":{\"type\":\"overloaded_error\",\"message\":\"Overloaded\"}}\n\n"))
	}))
	t.Cleanup(server.Close)
	t.Setenv("ANTHROPIC_BASE_URL", server.URL)
	r, mock := newMeteredAnthropic(t)

	stream, err := r.Messages().CreateMessageStream(context.Background(), messageParams("claude-haiku-4-5"))
	require.NoError(t, err)
	wrapper := stream.(*StreamingWrapper)
	for wrapper.Next() {
		wrapper.Current()
	}
	require.Error(t, wrapper.Err())
	_ = wrapper.Close()

	record := flushedRecords(t, r, mock, 1)[0]
	assert.Equal(t, "ERROR", record["stopReason"])
	assert.Equal(t, float64(25), record["inputTokenCount"])
	assert.Equal(t, float64(1), record["outputTokenCount"], "message_start reported one output token")
	assert.Equal(t, float64(26), record["totalTokenCount"])
	assert.Nil(t, record["errorCode"], "the stream itself was answered with 200")
}

func TestFailedStreamThatReportedZeroInputTokensIsNotBilledForTheEstimate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_0\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"claude-haiku-4-5\",\"content\":[],\"stop_reason\":null,\"usage\":{\"input_tokens\":0,\"output_tokens\":0}}}\n\n"))
		_, _ = w.Write([]byte("event: error\ndata: {\"type\":\"error\",\"error\":{\"type\":\"overloaded_error\",\"message\":\"Overloaded\"}}\n\n"))
	}))
	t.Cleanup(server.Close)
	t.Setenv("ANTHROPIC_BASE_URL", server.URL)
	r, mock := newMeteredAnthropic(t)

	stream, err := r.Messages().CreateMessageStream(context.Background(), messageParams("claude-haiku-4-5"))
	require.NoError(t, err)
	wrapper := stream.(*StreamingWrapper)
	for wrapper.Next() {
		wrapper.Current()
	}
	require.Error(t, wrapper.Err())
	_ = wrapper.Close()

	record := flushedRecords(t, r, mock, 1)[0]
	assert.Equal(t, "ERROR", record["stopReason"])
	assert.Equal(t, float64(0), record["inputTokenCount"], "Anthropic reported 0 input tokens; the request-size estimate must not be billed")
}

func serveTurnThatBreaksOffAfterItsUsage(t *testing.T, iterations string) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		events := turnStreamEvents(iterations)
		for _, event := range events[:len(events)-1] {
			var typed struct {
				Type string `json:"type"`
			}
			_ = json.Unmarshal([]byte(event), &typed)
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", typed.Type, event)
		}
		fmt.Fprint(w, "event: error\ndata: {\"type\":\"error\",\"error\":{\"type\":\"overloaded_error\",\"message\":\"Overloaded\"}}\n\n")
	}))
	t.Cleanup(server.Close)
	t.Setenv("ANTHROPIC_BASE_URL", server.URL)
}

func TestStreamThatBreaksOffAfterReportingPassesStillRecordsThem(t *testing.T) {
	surfaces := map[string]func(t *testing.T, r *ReveniumAnthropic){
		"messages stream": func(t *testing.T, r *ReveniumAnthropic) {
			stream, err := r.Messages().CreateMessageStream(context.Background(), messageParams("claude-haiku-4-5"))
			require.NoError(t, err)
			wrapper := stream.(*StreamingWrapper)
			for wrapper.Next() {
				wrapper.Current()
			}
			require.Error(t, wrapper.Err())
			_ = wrapper.Close()
		},
		"beta messages stream": func(t *testing.T, r *ReveniumAnthropic) {
			stream, err := r.BetaMessages().CreateMessageStream(context.Background(), advisedBetaParams())
			require.NoError(t, err)
			for stream.Next() {
				_ = stream.Current()
			}
			require.Error(t, stream.Err())
			_ = stream.Close()
		},
	}
	for name, call := range surfaces {
		t.Run(name, func(t *testing.T) {
			serveTurnThatBreaksOffAfterItsUsage(t, advisedIterations)
			r, mock := newMeteredAnthropic(t)

			call(t, r)

			records := flushedRecords(t, r, mock, 3)
			bySubtype := recordsBySubtype(records)
			require.Contains(t, bySubtype, "advisor_message")
			require.Contains(t, bySubtype, "compaction")
			var main map[string]interface{}
			for _, record := range records {
				if record["operationSubtype"] == nil {
					main = record
				}
			}
			require.NotNil(t, main)
			assert.Equal(t, "ERROR", main["stopReason"])
		})
	}
}

func TestBedrockFailureWhoseFallbackSendsNothing_IsRecordedAsTheBedrockCall(t *testing.T) {
	stub := newBedrockStub(t)
	stub.rejectWith(http.StatusForbidden, "UnrecognizedClientException")
	anthropicAPI := newProviderStub(t)
	t.Setenv("ANTHROPIC_BASE_URL", anthropicAPI.server.URL)
	mock := newMeteringMock(t)
	cfg := bedrockConfig(mock.URL())
	WithBedrockFallbackToAnthropic(true)(cfg)
	r := newBedrockAnthropic(t, cfg)
	unconvertible := messageParams("arn:aws:bedrock:us-east-1:123456789012:custom-model")

	_, err := r.Messages().CreateMessage(context.Background(), unconvertible)
	require.ErrorIs(t, err, errFallbackModel)
	stream, err := r.Messages().CreateMessageStream(context.Background(), unconvertible)
	require.ErrorIs(t, err, errFallbackModel)
	require.Nil(t, stream)

	assert.Len(t, stub.received(), 2, "both Bedrock requests were sent")
	assert.Zero(t, anthropicAPI.hits.Load(), "the fallback sent nothing")
	r.Flush()
	records := mock.GetPayloads()
	require.Len(t, records, 2, "each Bedrock request that was sent and failed is recorded once")
	for _, record := range records {
		assert.Equal(t, "ERROR", record["stopReason"])
		assert.Equal(t, "Amazon Bedrock", record["provider"])
		assert.Equal(t, "UnrecognizedClientException (HTTP 403)", record["errorReason"])
	}
}

func TestStreamErrorEventPassedOnAsPlainText_IsTheProvidersReply(t *testing.T) {
	t.Setenv("REVENIUM_CAPTURE_ERROR_MESSAGES", "true")
	t.Setenv("REVENIUM_CAPTURE_PROMPTS", "")
	err := fmt.Errorf(streamErrorEventPrefix + `{"type":"error","error":{"type":"overloaded_error","message":"failed on 'say hello please'"}}`)

	failure := callFailure(err)

	assert.True(t, failure.ProviderReply)
	assert.Equal(t, "overloaded_error", failure.Reason(false), "the provider's text needs prompt capture as well")
}
