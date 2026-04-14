package anthropic

import "github.com/revenium/revenium-go-sdk/core"

func MapStopReasonToRevenium(stopReason string) string {
	switch stopReason {
	case "end_turn":
		return "END"
	case "max_tokens", "model_context_window_exceeded":
		return "TOKEN_LIMIT"
	case "stop_sequence":
		return "END_SEQUENCE"
	case "tool_use":
		return "END"
	case "pause_turn":
		return "END"
	case "refusal":
		return "ERROR"
	case "timeout":
		return "TIMEOUT"
	case "error":
		return "ERROR"
	case "cancelled", "canceled":
		return "CANCELLED"
	default:
		core.Debug("Unknown stop reason '%s', defaulting to END", stopReason)
		return "END"
	}
}

func NormalizeProviderName(provider string) string {
	switch provider {
	case "AWS":
		return "Amazon Bedrock"
	default:
		return provider
	}
}
