package google

import (
	"strings"

	"github.com/revenium/revenium-go-sdk/core"
	"google.golang.org/genai"
)

// MapGoogleFinishReason maps Google AI/Vertex AI finishReason to Revenium stopReason
//
// SPECIFICATION REFERENCES:
//   - Google AI finishReason enum:
//     https://cloud.google.com/vertex-ai/generative-ai/docs/reference/rest/v1/GenerateContentResponse
//   - Revenium Metering API stopReason field (required):
//     https://revenium.readme.io/reference/meter_ai_completion
//
// MAPPING RATIONALE:
// - STOP (natural completion) -> END
// - MAX_TOKENS (hit limit) -> TOKEN_LIMIT
// - Safety/content blocks -> ERROR (catches all policy violations)
// - Function call errors -> ERROR (invalid tool usage)
// - CANCELLED/CANCELED -> CANCELLED (handles both spellings)
// - Unknown/future values -> fallback with warning (resilience)
//
// RESILIENCE GUARANTEES:
// - Never panics - always returns a valid Revenium enum value
// - Handles empty strings gracefully
// - Gracefully maps unknown/future Google values with warning
func MapGoogleFinishReason(finishReason genai.FinishReason, defaultReason core.ReveniumStopReason) core.ReveniumStopReason {
	// Handle empty finish reason
	if finishReason == "" {
		return defaultReason
	}

	// Normalize to uppercase for case-insensitive matching
	normalizedReason := strings.ToUpper(string(finishReason))

	// Map Google finish reasons to Revenium stop reasons
	switch normalizedReason {
	// Natural completion
	case "STOP":
		return core.StopReasonEnd

	// Token limits
	case "MAX_TOKENS":
		return core.StopReasonTokenLimit

	// Safety and content filtering (map to ERROR)
	case "SAFETY",
		"RECITATION",
		"BLOCKLIST",
		"PROHIBITED_CONTENT",
		"SPII",
		"MODEL_ARMOR",
		"IMAGE_SAFETY",
		"IMAGE_PROHIBITED_CONTENT",
		"IMAGE_RECITATION":
		return core.StopReasonError

	// Function call errors (map to ERROR)
	case "MALFORMED_FUNCTION_CALL",
		"UNEXPECTED_TOOL_CALL",
		"NO_IMAGE":
		return core.StopReasonError

	// Cancellation (handle both American and British spellings)
	case "CANCELLED", "CANCELED":
		return core.StopReasonCancelled

	// Unspecified or other reasons (use default)
	case "FINISH_REASON_UNSPECIFIED", "OTHER", "IMAGE_OTHER":
		return defaultReason

	// Unknown finish reason (future-proof for new Google values)
	default:
		core.Warn("Unknown finishReason: %q. Using fallback: %q. Please report this to support@revenium.io if this is a new Google AI value.", finishReason, defaultReason)
		return defaultReason
	}
}

// ExtractFinishReason extracts finishReason from Google AI response structure
// Handles both streaming and non-streaming response formats
//
// RESILIENCE GUARANTEES:
// - Never panics - returns empty string if extraction fails
// - Handles nil response objects
//
// Returns the finishReason or empty string if not found
func ExtractFinishReason(response *genai.GenerateContentResponse) genai.FinishReason {
	if response == nil {
		return ""
	}

	// Try candidates array (most common format)
	if len(response.Candidates) > 0 && response.Candidates[0] != nil {
		return response.Candidates[0].FinishReason
	}

	return ""
}
