package openai

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/openai/openai-go/v3/responses"
	"github.com/revenium/revenium-go-sdk/core/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const responsesIncomplete = `{"type":"response.incomplete","sequence_number":17,"response":{"id":"resp_2","object":"response","created_at":1700000000,"model":"gpt-4.1-nano","status":"incomplete","incomplete_details":{"reason":"max_output_tokens"},"error":null,"output":[],"usage":{"input_tokens":19,"input_tokens_details":{"cache_write_tokens":0,"cached_tokens":4},"output_tokens":16,"output_tokens_details":{"reasoning_tokens":5},"total_tokens":35}}}`

const responsesIncompleteContentFilter = `{"type":"response.incomplete","sequence_number":17,"response":{"id":"resp_4","object":"response","created_at":1700000000,"model":"gpt-4.1-nano","status":"incomplete","incomplete_details":{"reason":"content_filter"},"error":null,"output":[],"usage":{"input_tokens":19,"input_tokens_details":{"cache_write_tokens":0,"cached_tokens":4},"output_tokens":16,"output_tokens_details":{"reasoning_tokens":5},"total_tokens":35}}}`

const responsesFailed = `{"type":"response.failed","sequence_number":3,"response":{"id":"resp_3","object":"response","created_at":1700000000,"model":"gpt-4.1-nano","status":"failed","incomplete_details":null,"error":{"code":"server_error","message":"The model failed to finish the response."},"output":[],"usage":{"input_tokens":12,"input_tokens_details":{"cached_tokens":0},"output_tokens":6,"output_tokens_details":{"reasoning_tokens":0},"total_tokens":18}}}`

func TestResponsesStreaming_FinalEventSetsUsageAndStopReason(t *testing.T) {
	cases := []struct {
		name            string
		finalEvent      string
		wantInput       float64
		wantOutput      float64
		wantTotal       float64
		wantReasoning   float64
		wantCacheRead   float64
		wantStopReason  string
		wantErrorReason interface{}
	}{
		{name: "completed", finalEvent: responsesCompleted, wantInput: 9, wantOutput: 2, wantTotal: 11, wantStopReason: "END"},
		{name: "incomplete at the output limit", finalEvent: responsesIncomplete, wantInput: 19, wantOutput: 16, wantTotal: 35, wantReasoning: 5, wantCacheRead: 4, wantStopReason: "TOKEN_LIMIT"},
		{name: "incomplete because of the content filter", finalEvent: responsesIncompleteContentFilter, wantInput: 19, wantOutput: 16, wantTotal: 35, wantReasoning: 5, wantCacheRead: 4, wantStopReason: "ERROR"},
		{name: "failed", finalEvent: responsesFailed, wantInput: 12, wantOutput: 6, wantTotal: 18, wantStopReason: "ERROR", wantErrorReason: "server_error"},
	}

	readModes := []struct {
		name string
		read func(stream *ResponsesStreamingWrapper)
	}{
		{name: "reading every event", read: func(stream *ResponsesStreamingWrapper) {
			for stream.Next() {
				stream.Current()
			}
		}},
		{name: "draining with Next only", read: func(stream *ResponsesStreamingWrapper) {
			for stream.Next() {
			}
		}},
	}

	for _, mode := range readModes {
		for _, tc := range cases {
			t.Run(mode.name+"/"+tc.name, func(t *testing.T) {
				assertFinalEventMetered(t, tc.finalEvent, mode.read, finalEventExpectation{
					input: tc.wantInput, output: tc.wantOutput, total: tc.wantTotal,
					reasoning: tc.wantReasoning, cacheRead: tc.wantCacheRead,
					stopReason: tc.wantStopReason, errorReason: tc.wantErrorReason,
				})
			})
		}
	}
}

type finalEventExpectation struct {
	input, output, total, reasoning, cacheRead float64
	stopReason                                 string
	errorReason                                interface{}
}

func assertFinalEventMetered(t *testing.T, finalEvent string, read func(*ResponsesStreamingWrapper), want finalEventExpectation) {
	t.Helper()
	providerServer := httptest.NewServer(sseResponsesHandler(t, responsesTextDelta, finalEvent))
	defer providerServer.Close()

	mock := testutil.NewMockMeteringServer()
	defer mock.Close()

	r := newTestOpenAI(t, providerServer.URL, mock.URL())
	defer r.Close()

	stream, err := r.Responses().CreateStreaming(context.Background(), responses.ResponseNewParams{Model: "gpt-4.1-nano"})
	require.NoError(t, err)
	read(stream)
	require.NoError(t, stream.Err())
	require.NoError(t, stream.Close())
	r.Flush()

	require.True(t, mock.WaitForPayloads(1, 2*time.Second))
	payloads := mock.GetPayloads()
	require.Len(t, payloads, 1)
	p := payloads[0]
	assert.Equal(t, true, p["isStreamed"])
	assert.Equal(t, "gpt-4.1-nano", p["model"])
	assert.Equal(t, want.input, p["inputTokenCount"])
	assert.Equal(t, want.output, p["outputTokenCount"])
	assert.Equal(t, want.total, p["totalTokenCount"])
	assert.Equal(t, want.reasoning, p["reasoningTokenCount"])
	assert.Equal(t, want.cacheRead, p["cacheReadTokenCount"])
	assert.Equal(t, want.stopReason, p["stopReason"])
	assert.Equal(t, want.errorReason, p["errorReason"])
}
