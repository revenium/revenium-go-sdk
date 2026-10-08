package perplexity

import (
	"github.com/revenium/revenium-go-sdk/core"
	"strings"
)

type ReveniumStopReason = core.ReveniumStopReason

const (
	StopReasonEnd             = core.StopReasonEnd
	StopReasonEndSequence     = core.StopReasonEndSequence
	StopReasonTimeout         = core.StopReasonTimeout
	StopReasonTokenLimit      = core.StopReasonTokenLimit
	StopReasonCostLimit       = core.StopReasonCostLimit
	StopReasonCompletionLimit = core.StopReasonCompletionLimit
	StopReasonError           = core.StopReasonError
	StopReasonCancelled       = core.StopReasonCancelled
)

// MapOpenAIFinishReason maps OpenAI/Perplexity finishReason to Revenium stopReason
//
// SPECIFICATION REFERENCES:
//   - OpenAI finishReason enum:
//     https://platform.openai.com/docs/api-reference/chat/object
//   - Perplexity uses the same finishReason semantics as OpenAI.
//   - Revenium Metering API stopReason field (required):
//     https://revenium.readme.io/reference/meter_ai_completion
//
// RESILIENCE GUARANTEES:
// - Never panics - always returns a valid Revenium enum value
// - Handles empty strings gracefully
// - Gracefully maps unknown/future OpenAI values with warning
func MapOpenAIFinishReason(finishReason string, defaultReason ReveniumStopReason) ReveniumStopReason {
	// Handle empty finish reason
	if finishReason == "" {
		return defaultReason
	}

	// Normalize to uppercase for case-insensitive matching
	normalizedReason := strings.ToUpper(finishReason)

	// Map OpenAI finish reasons to Revenium stop reasons
	switch normalizedReason {
	case "STOP":
		return StopReasonEnd

	case "LENGTH":
		return StopReasonTokenLimit

	case "CONTENT_FILTER":
		return StopReasonError

	case "TOOL_CALLS", "FUNCTION_CALL":
		return StopReasonEndSequence

	default:
		core.Warn("Unknown finishReason: %q. Using fallback: %q. Please report this to support@revenium.io if this is a new OpenAI/Perplexity value.", finishReason, defaultReason)
		return defaultReason
	}
}
