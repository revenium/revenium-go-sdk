package openai

import (
	"testing"
	"time"

	"github.com/openai/openai-go/v3/responses"
	"github.com/stretchr/testify/assert"
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

	payload := iface.buildResponsePayload(toolCallResponse(), nil, 250*time.Millisecond, "OPENAI", requestTime, nil)

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

	payload := iface.buildResponsePayload(toolCallResponse(), nil, 250*time.Millisecond, "OPENAI", requestTime, timing)

	assert.Equal(t, "END_SEQUENCE", payload.StopReason)
	assert.True(t, payload.IsStreamed)
	assert.Equal(t, int64(40), payload.TimeToFirstToken)
}

func TestStreamingWrapperCapturesCompletedResponseOutput(t *testing.T) {
	wrapper := &ResponsesStreamingWrapper{iface: &ResponsesInterface{}, startTime: time.Now()}
	completed := toolCallResponse()

	wrapper.captureEvent(responses.ResponseStreamEventUnion{Type: "response.completed", Response: *completed})

	assert.NotNil(t, wrapper.finalResponse)
	payload := wrapper.iface.buildResponsePayload(wrapper.finalResponse, nil, time.Second, "OPENAI", wrapper.startTime, &streamTiming{})
	assert.Equal(t, "END_SEQUENCE", payload.StopReason)
}
