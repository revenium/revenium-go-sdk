package anthropic

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMapStopReasonToRevenium(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"end_turn", "END"},
		{"max_tokens", "TOKEN_LIMIT"},
		{"model_context_window_exceeded", "TOKEN_LIMIT"},
		{"stop_sequence", "END_SEQUENCE"},
		{"tool_use", "END"},
		{"pause_turn", "END"},
		{"refusal", "ERROR"},
		{"timeout", "TIMEOUT"},
		{"error", "ERROR"},
		{"cancelled", "CANCELLED"},
		{"canceled", "CANCELLED"},
		{"unknown_reason", "END"},
		{"", "END"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.expected, MapStopReasonToRevenium(tt.input))
		})
	}
}

func TestNormalizeProviderName(t *testing.T) {
	assert.Equal(t, "Amazon Bedrock", NormalizeProviderName("AWS"))
	assert.Equal(t, "Anthropic", NormalizeProviderName("Anthropic"))
	assert.Equal(t, "Custom", NormalizeProviderName("Custom"))
}
