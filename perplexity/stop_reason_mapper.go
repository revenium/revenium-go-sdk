package perplexity

import (
	"github.com/revenium/revenium-go-sdk/core"
	"strings"
)

// ReveniumStopReason represents the standardized stop reasons for Revenium metering API
type ReveniumStopReason string

const (
	StopReasonEnd             ReveniumStopReason = "END"
	StopReasonEndSequence     ReveniumStopReason = "END_SEQUENCE"
	StopReasonTimeout         ReveniumStopReason = "TIMEOUT"
	StopReasonTokenLimit      ReveniumStopReason = "TOKEN_LIMIT"
	StopReasonCostLimit       ReveniumStopReason = "COST_LIMIT"
	StopReasonCompletionLimit ReveniumStopReason = "COMPLETION_LIMIT"
	StopReasonError           ReveniumStopReason = "ERROR"
	StopReasonCancelled       ReveniumStopReason = "CANCELLED"
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
// MAPPING RATIONALE:
// - stop (natural completion) -> END
// - length (hit token limit) -> TOKEN_LIMIT
// - content_filter (safety/policy violation) -> ERROR
// - tool_calls/function_call (tool usage) -> END (normal completion with tools)
// - Unknown/future values -> fallback with warning (resilience)
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
	// Natural completion
	case "STOP":
		return StopReasonEnd

	// Token limits
	case "LENGTH":
		return StopReasonTokenLimit

	// Content filtering (map to ERROR)
	case "CONTENT_FILTER":
		return StopReasonError

	// Tool/function calls (normal completion)
	case "TOOL_CALLS", "FUNCTION_CALL":
		return StopReasonEnd

	// Unknown finish reason (future-proof for new OpenAI values)
	default:
		core.Warn("Unknown finishReason: %q. Using fallback: %q. Please report this to support@revenium.io if this is a new OpenAI/Perplexity value.", finishReason, defaultReason)
		return defaultReason
	}
}
