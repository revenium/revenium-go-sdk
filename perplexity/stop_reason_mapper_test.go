package perplexity

import "testing"

func TestMapOpenAIFinishReason(t *testing.T) {
	tests := []struct {
		name           string
		finishReason   string
		defaultReason  ReveniumStopReason
		expectedReason ReveniumStopReason
	}{
		// Natural completion
		{
			name:           "stop maps to END",
			finishReason:   "stop",
			defaultReason:  StopReasonEnd,
			expectedReason: StopReasonEnd,
		},
		{
			name:           "STOP (uppercase) maps to END",
			finishReason:   "STOP",
			defaultReason:  StopReasonEnd,
			expectedReason: StopReasonEnd,
		},
		// Token limits
		{
			name:           "length maps to TOKEN_LIMIT",
			finishReason:   "length",
			defaultReason:  StopReasonEnd,
			expectedReason: StopReasonTokenLimit,
		},
		{
			name:           "LENGTH (uppercase) maps to TOKEN_LIMIT",
			finishReason:   "LENGTH",
			defaultReason:  StopReasonEnd,
			expectedReason: StopReasonTokenLimit,
		},
		// Content filtering
		{
			name:           "content_filter maps to ERROR",
			finishReason:   "content_filter",
			defaultReason:  StopReasonEnd,
			expectedReason: StopReasonError,
		},
		{
			name:           "CONTENT_FILTER (uppercase) maps to ERROR",
			finishReason:   "CONTENT_FILTER",
			defaultReason:  StopReasonEnd,
			expectedReason: StopReasonError,
		},
		// Tool/function calls
		{
			name:           "tool_calls maps to END",
			finishReason:   "tool_calls",
			defaultReason:  StopReasonEnd,
			expectedReason: StopReasonEnd,
		},
		{
			name:           "TOOL_CALLS (uppercase) maps to END",
			finishReason:   "TOOL_CALLS",
			defaultReason:  StopReasonEnd,
			expectedReason: StopReasonEnd,
		},
		{
			name:           "function_call maps to END",
			finishReason:   "function_call",
			defaultReason:  StopReasonEnd,
			expectedReason: StopReasonEnd,
		},
		{
			name:           "FUNCTION_CALL (uppercase) maps to END",
			finishReason:   "FUNCTION_CALL",
			defaultReason:  StopReasonEnd,
			expectedReason: StopReasonEnd,
		},
		// Empty finish reason
		{
			name:           "Empty finish reason uses default",
			finishReason:   "",
			defaultReason:  StopReasonEnd,
			expectedReason: StopReasonEnd,
		},
		{
			name:           "Empty finish reason uses custom default",
			finishReason:   "",
			defaultReason:  StopReasonTokenLimit,
			expectedReason: StopReasonTokenLimit,
		},
		// Unknown finish reason
		{
			name:           "Unknown finish reason uses default",
			finishReason:   "unknown_future_reason",
			defaultReason:  StopReasonEnd,
			expectedReason: StopReasonEnd,
		},
		{
			name:           "Unknown finish reason uses custom default",
			finishReason:   "some_new_openai_value",
			defaultReason:  StopReasonError,
			expectedReason: StopReasonError,
		},
		// Case insensitivity
		{
			name:           "Mixed case Stop maps to END",
			finishReason:   "Stop",
			defaultReason:  StopReasonEnd,
			expectedReason: StopReasonEnd,
		},
		{
			name:           "Mixed case Length maps to TOKEN_LIMIT",
			finishReason:   "Length",
			defaultReason:  StopReasonEnd,
			expectedReason: StopReasonTokenLimit,
		},
		{
			name:           "Mixed case Content_Filter maps to ERROR",
			finishReason:   "Content_Filter",
			defaultReason:  StopReasonEnd,
			expectedReason: StopReasonError,
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

