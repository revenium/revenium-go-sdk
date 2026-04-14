package openai

import (
	"testing"

	"github.com/revenium/revenium-go-sdk/core"
)

func TestMapOpenAIFinishReason(t *testing.T) {
	tests := []struct {
		name           string
		finishReason   string
		defaultReason  core.ReveniumStopReason
		expectedReason core.ReveniumStopReason
	}{
		{
			name:           "stop maps to END",
			finishReason:   "stop",
			defaultReason:  core.StopReasonEnd,
			expectedReason: core.StopReasonEnd,
		},
		{
			name:           "STOP (uppercase) maps to END",
			finishReason:   "STOP",
			defaultReason:  core.StopReasonEnd,
			expectedReason: core.StopReasonEnd,
		},
		{
			name:           "length maps to TOKEN_LIMIT",
			finishReason:   "length",
			defaultReason:  core.StopReasonEnd,
			expectedReason: core.StopReasonTokenLimit,
		},
		{
			name:           "LENGTH (uppercase) maps to TOKEN_LIMIT",
			finishReason:   "LENGTH",
			defaultReason:  core.StopReasonEnd,
			expectedReason: core.StopReasonTokenLimit,
		},
		{
			name:           "content_filter maps to ERROR",
			finishReason:   "content_filter",
			defaultReason:  core.StopReasonEnd,
			expectedReason: core.StopReasonError,
		},
		{
			name:           "CONTENT_FILTER (uppercase) maps to ERROR",
			finishReason:   "CONTENT_FILTER",
			defaultReason:  core.StopReasonEnd,
			expectedReason: core.StopReasonError,
		},
		{
			name:           "tool_calls maps to END",
			finishReason:   "tool_calls",
			defaultReason:  core.StopReasonEnd,
			expectedReason: core.StopReasonEnd,
		},
		{
			name:           "TOOL_CALLS (uppercase) maps to END",
			finishReason:   "TOOL_CALLS",
			defaultReason:  core.StopReasonEnd,
			expectedReason: core.StopReasonEnd,
		},
		{
			name:           "function_call maps to END",
			finishReason:   "function_call",
			defaultReason:  core.StopReasonEnd,
			expectedReason: core.StopReasonEnd,
		},
		{
			name:           "FUNCTION_CALL (uppercase) maps to END",
			finishReason:   "FUNCTION_CALL",
			defaultReason:  core.StopReasonEnd,
			expectedReason: core.StopReasonEnd,
		},
		{
			name:           "Empty finish reason uses default",
			finishReason:   "",
			defaultReason:  core.StopReasonEnd,
			expectedReason: core.StopReasonEnd,
		},
		{
			name:           "Empty finish reason uses custom default",
			finishReason:   "",
			defaultReason:  core.StopReasonTokenLimit,
			expectedReason: core.StopReasonTokenLimit,
		},
		{
			name:           "Unknown finish reason uses default",
			finishReason:   "unknown_future_reason",
			defaultReason:  core.StopReasonEnd,
			expectedReason: core.StopReasonEnd,
		},
		{
			name:           "Unknown finish reason uses custom default",
			finishReason:   "some_new_openai_value",
			defaultReason:  core.StopReasonError,
			expectedReason: core.StopReasonError,
		},
		{
			name:           "Mixed case Stop maps to END",
			finishReason:   "Stop",
			defaultReason:  core.StopReasonEnd,
			expectedReason: core.StopReasonEnd,
		},
		{
			name:           "Mixed case Length maps to TOKEN_LIMIT",
			finishReason:   "Length",
			defaultReason:  core.StopReasonEnd,
			expectedReason: core.StopReasonTokenLimit,
		},
		{
			name:           "Mixed case Content_Filter maps to ERROR",
			finishReason:   "Content_Filter",
			defaultReason:  core.StopReasonEnd,
			expectedReason: core.StopReasonError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := MapOpenAIFinishReason(tt.finishReason, tt.defaultReason)
			if result != tt.expectedReason {
				t.Errorf("MapOpenAIFinishReason(%q, %q) = %q, want %q",
					tt.finishReason, tt.defaultReason, result, tt.expectedReason)
			}
		})
	}
}
