package ollama

import "github.com/revenium/revenium-go-sdk/core"

func mapStopReasonToRevenium(stopReason string) string {
	switch stopReason {
	case "stop":
		return "END"
	case "length":
		return "TOKEN_LIMIT"
	case "tool_calls", "function_call":
		return "END"
	case "content_filter":
		return "ERROR"
	case "null":
		return "END"
	default:
		core.Debug("Unknown stop reason '%s', defaulting to END", stopReason)
		return "END"
	}
}
