package litellm

import (
	"strings"

	"github.com/revenium/revenium-go-sdk/core"
)

// MapFinishReason maps LLM finishReason to Revenium stopReason.
// This handles finish reasons from any provider routed through LiteLLM,
// as LiteLLM normalizes them to the OpenAI format.
func MapFinishReason(finishReason string, defaultReason core.ReveniumStopReason) core.ReveniumStopReason {
	if finishReason == "" {
		return defaultReason
	}

	normalizedReason := strings.ToUpper(finishReason)

	switch normalizedReason {
	case "STOP":
		return core.StopReasonEnd
	case "LENGTH":
		return core.StopReasonTokenLimit
	case "CONTENT_FILTER":
		return core.StopReasonError
	case "TOOL_CALLS", "FUNCTION_CALL":
		return core.StopReasonEnd
	case "ERROR":
		return core.StopReasonError
	case "CANCELLED":
		return core.StopReasonCancelled
	default:
		core.Warn("Unknown finishReason: %q. Using fallback: %q.", finishReason, defaultReason)
		return defaultReason
	}
}
