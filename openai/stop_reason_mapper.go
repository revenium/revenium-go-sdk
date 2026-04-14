package openai

import (
	"strings"

	"github.com/revenium/revenium-go-sdk/core"
)

// MapOpenAIFinishReason maps OpenAI/Azure OpenAI finishReason to Revenium stopReason
//
// SPECIFICATION REFERENCES:
//   - OpenAI finishReason enum:
//     https://platform.openai.com/docs/api-reference/chat/object
//   - Revenium Metering API stopReason field (required):
//     https://revenium.readme.io/reference/meter_ai_completion
//
// RESILIENCE GUARANTEES:
//   - Never panics - always returns a valid Revenium enum value
//   - Handles empty strings gracefully
//   - Gracefully maps unknown/future OpenAI values with warning
func MapOpenAIFinishReason(finishReason string, defaultReason core.ReveniumStopReason) core.ReveniumStopReason {
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
	default:
		core.Warn("Unknown finishReason: %q. Using fallback: %q. Please report this to support@revenium.io if this is a new OpenAI value.", finishReason, defaultReason)
		return defaultReason
	}
}
