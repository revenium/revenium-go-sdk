package metering

import (
	"encoding/json"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/revenium/revenium-go-sdk/core/prompt"
)

const systemPromptKey = "systemPrompt"

// ApplyCapturedPrompt stores a captured prompt and answer on payload with credentials redacted and
// each cut to the REVENIUM_MAX_PROMPT_SIZE limit, and sets PromptsTruncated when either was cut.
// The prompt is stored as inputMessages; empty values are left unset.
func ApplyCapturedPrompt(payload *MeteringPayload, promptText, output string) {
	maxSize := prompt.GetMaxPromptSize()
	if promptText != "" {
		bounded, cut := boundedCapture(promptText, maxSize)
		inputMessages, truncated := FormatPromptAsInputMessages(bounded)
		if inputMessages != "" {
			payload.InputMessages = inputMessages
		}
		payload.PromptsTruncated = cut || truncated
	}
	if output != "" {
		bounded, cut := boundedCapture(output, maxSize)
		payload.OutputResponse = bounded
		payload.PromptsTruncated = payload.PromptsTruncated || cut
	}
}

// boundedCapture is the redact-then-cut step for the captured text fields (system prompt,
// input prompt, reply), so credentials are masked before any text is dropped. Input
// messages are masked value by value and cut by prompt.SerializeMessagesWithinLimit.
func boundedCapture(text string, maxSize int) (string, bool) {
	return withinLimit(prompt.SanitizeCredentials(text), maxSize)
}

func withinLimit(sanitized string, maxSize int) (string, bool) {
	bounded := prompt.TruncateString(sanitized, maxSize)
	return bounded, bounded != sanitized
}

// applySystemPrompt stores the systemPrompt from usage metadata the way a
// captured prompt is stored: only when prompt capture is on for the call, with
// credentials redacted and cut to the REVENIUM_MAX_PROMPT_SIZE limit.
func applySystemPrompt(payload *MeteringPayload, metadata map[string]interface{}) {
	text, ok := metadata[systemPromptKey].(string)
	if !ok || core.IsBlank(text) || !prompt.ShouldCapturePrompts(metadata) {
		return
	}
	bounded, cut := boundedCapture(text, prompt.GetMaxPromptSize())
	payload.SystemPrompt = bounded
	payload.PromptsTruncated = payload.PromptsTruncated || cut
}

// applyBuilderCapture has no call metadata to read, so only the process-wide capture setting decides.
func applyBuilderCapture(payload *MeteringPayload, input, output string, truncated bool) {
	if !prompt.ShouldCapturePrompts(nil) {
		return
	}
	ApplyChatCapture(payload, nil, func() ChatCapture {
		return ChatCapture{Messages: capturedInputMessages(input), Output: output}
	})
	if payload.InputMessages != "" || payload.OutputResponse != "" {
		payload.PromptsTruncated = payload.PromptsTruncated || truncated
	}
}

func capturedInputMessages(input string) []interface{} {
	if core.IsBlank(input) {
		return nil
	}
	var messages []interface{}
	if err := json.Unmarshal([]byte(input), &messages); err == nil {
		list, _ := prompt.WithoutInlineData(messages).([]interface{})
		return list
	}
	return []interface{}{map[string]interface{}{"role": "user", "content": input}}
}
