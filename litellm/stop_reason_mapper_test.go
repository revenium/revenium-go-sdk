package litellm

import (
	"testing"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/stretchr/testify/assert"
)

func TestMapFinishReason(t *testing.T) {
	tests := []struct {
		name     string
		reason   string
		default_ core.ReveniumStopReason
		expected core.ReveniumStopReason
	}{
		{"stop", "stop", core.StopReasonEnd, core.StopReasonEnd},
		{"STOP uppercase", "STOP", core.StopReasonEnd, core.StopReasonEnd},
		{"length", "length", core.StopReasonEnd, core.StopReasonTokenLimit},
		{"content_filter", "content_filter", core.StopReasonEnd, core.StopReasonError},
		{"tool_calls", "tool_calls", core.StopReasonEnd, core.StopReasonEnd},
		{"function_call", "function_call", core.StopReasonEnd, core.StopReasonEnd},
		{"error", "error", core.StopReasonEnd, core.StopReasonError},
		{"cancelled", "cancelled", core.StopReasonEnd, core.StopReasonCancelled},
		{"empty uses default", "", core.StopReasonEnd, core.StopReasonEnd},
		{"empty uses custom default", "", core.StopReasonTokenLimit, core.StopReasonTokenLimit},
		{"unknown uses default", "new_unknown_reason", core.StopReasonEnd, core.StopReasonEnd},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := MapFinishReason(tt.reason, tt.default_)
			assert.Equal(t, tt.expected, result)
		})
	}
}
