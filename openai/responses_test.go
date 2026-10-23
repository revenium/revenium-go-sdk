package openai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/openai/openai-go/v3/responses"
	"github.com/revenium/revenium-go-sdk/core/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMapResponseStopReason(t *testing.T) {
	tests := []struct {
		name     string
		resp     responses.Response
		expected string
	}{
		{
			name:     "completed without tool calls maps to END",
			resp:     responses.Response{Status: "completed"},
			expected: "END",
		},
		{
			name: "completed with function call maps to END_SEQUENCE",
			resp: responses.Response{
				Status: "completed",
				Output: []responses.ResponseOutputItemUnion{{Type: "message"}, {Type: "function_call"}},
			},
			expected: "END_SEQUENCE",
		},
		{
			name: "completed with custom tool call maps to END_SEQUENCE",
			resp: responses.Response{
				Status: "completed",
				Output: []responses.ResponseOutputItemUnion{{Type: "custom_tool_call"}},
			},
			expected: "END_SEQUENCE",
		},
		{
			name: "incomplete with function call keeps the status mapping",
			resp: responses.Response{
				Status: "incomplete",
				Output: []responses.ResponseOutputItemUnion{{Type: "function_call"}},
			},
			expected: "TOKEN_LIMIT",
		},
		{
			name: "incomplete at the output limit maps to TOKEN_LIMIT",
			resp: responses.Response{
				Status:            "incomplete",
				IncompleteDetails: responses.ResponseIncompleteDetails{Reason: "max_output_tokens"},
			},
			expected: "TOKEN_LIMIT",
		},
		{
			name: "incomplete because of the content filter maps to ERROR",
			resp: responses.Response{
				Status:            "incomplete",
				IncompleteDetails: responses.ResponseIncompleteDetails{Reason: "content_filter"},
			},
			expected: "ERROR",
		},
		{
			name:     "failed maps to ERROR",
			resp:     responses.Response{Status: "failed"},
			expected: "ERROR",
		},
		{
			name:     "cancelled maps to CANCELLED",
			resp:     responses.Response{Status: "cancelled"},
			expected: "CANCELLED",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, mapResponseStopReason(&tt.resp))
		})
	}
}

func toolCallResponse() *responses.Response {
	return &responses.Response{
		Model:  "gpt-5",
		Status: "completed",
		Output: []responses.ResponseOutputItemUnion{{Type: "message"}, {Type: "function_call"}},
		Usage: responses.ResponseUsage{
			InputTokens:  11,
			OutputTokens: 7,
			TotalTokens:  18,
		},
	}
}

func TestBuildResponsePayloadMetersToolCallAsEndSequence(t *testing.T) {
	iface := &ResponsesInterface{}
	requestTime := time.Now()

	payload := iface.buildResponsePayload(toolCallResponse(), "gpt-5", nil, 250*time.Millisecond, "OPENAI", requestTime, nil)

	assert.Equal(t, "END_SEQUENCE", payload.StopReason)
	assert.False(t, payload.IsStreamed)
	assert.Equal(t, int64(11), payload.InputTokenCount)
	assert.Equal(t, int64(7), payload.OutputTokenCount)
	assert.Equal(t, "gpt-5", payload.Model)
}

func TestBuildResponsePayloadMetersStreamedToolCallAsEndSequence(t *testing.T) {
	iface := &ResponsesInterface{}
	requestTime := time.Now()
	completionStartTime := requestTime.Add(40 * time.Millisecond)
	timing := &streamTiming{timeToFirstToken: 40, completionStartTime: &completionStartTime}

	payload := iface.buildResponsePayload(toolCallResponse(), "gpt-5", nil, 250*time.Millisecond, "OPENAI", requestTime, timing)

	assert.Equal(t, "END_SEQUENCE", payload.StopReason)
	assert.True(t, payload.IsStreamed)
	assert.Equal(t, int64(40), payload.TimeToFirstToken)
}

func TestStreamingWrapperCapturesCompletedResponseOutput(t *testing.T) {
	wrapper := &ResponsesStreamingWrapper{iface: &ResponsesInterface{}, clock: startStreamClock()}
	completed := toolCallResponse()

	wrapper.captureEvent(responses.ResponseStreamEventUnion{Type: "response.completed", Response: *completed})

	assert.NotNil(t, wrapper.finalResponse)
	payload := wrapper.iface.buildResponsePayload(wrapper.finalResponse, wrapper.model, nil, time.Second, "OPENAI", wrapper.clock.start, &streamTiming{})
	assert.Equal(t, "END_SEQUENCE", payload.StopReason)
}

func TestResponsesCreate_RecordsTheIncompleteReasonAsTheStopReason(t *testing.T) {
	cases := []struct {
		reason         string
		wantStopReason string
	}{
		{reason: "max_output_tokens", wantStopReason: "TOKEN_LIMIT"},
		{reason: "content_filter", wantStopReason: "ERROR"},
	}
	for _, tc := range cases {
		t.Run(tc.reason, func(t *testing.T) {
			providerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"id":"resp_1","object":"response","created_at":1700000000,"model":"gpt-4.1-nano",` +
					`"status":"incomplete","incomplete_details":{"reason":"` + tc.reason + `"},"error":null,"output":[],` +
					`"usage":{"input_tokens":19,"input_tokens_details":{"cached_tokens":0},"output_tokens":16,` +
					`"output_tokens_details":{"reasoning_tokens":0},"total_tokens":35}}`))
			}))
			defer providerServer.Close()
			mock := testutil.NewMockMeteringServer()
			defer mock.Close()
			r := newTestOpenAI(t, providerServer.URL, mock.URL())
			defer r.Close()

			_, err := r.Responses().Create(context.Background(), responses.ResponseNewParams{Model: "gpt-4.1-nano"})
			require.NoError(t, err)
			r.Flush()

			require.True(t, mock.WaitForPayloads(1, 2*time.Second))
			p := mock.GetPayloads()[0]
			assert.Equal(t, tc.wantStopReason, p["stopReason"])
			assert.Equal(t, float64(19), p["inputTokenCount"])
			assert.Equal(t, float64(16), p["outputTokenCount"])
		})
	}
}
