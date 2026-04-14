package fal

import (
	"encoding/json"
	"time"

	"github.com/revenium/revenium-go-sdk/core/metering"
)

func buildChatMeteringPayload(
	model string,
	result map[string]interface{},
	metadata map[string]interface{},
	duration time.Duration,
	requestTime time.Time,
	capturePrompts bool,
	prompt string,
) *metering.MeteringPayload {
	b := metering.NewPayload(metering.OperationChat, normalizeModelName(model), "fal_ai").
		WithTiming(requestTime, duration).
		WithModelSource("FAL")

	inputTokens, outputTokens, totalTokens := extractChatUsage(result)
	if totalTokens > 0 || inputTokens > 0 || outputTokens > 0 {
		b.WithTokens(inputTokens, outputTokens, totalTokens)
	}

	payload := b.Build()
	metering.ApplyMetadata(payload, metadata)

	if capturePrompts {
		applyCapturedPrompt(payload, prompt, chatOutputFromResult(result))
	}

	return payload
}

func extractChatUsage(result map[string]interface{}) (input, output, total int64) {
	usage, ok := result["usage"].(map[string]interface{})
	if !ok {
		return 0, 0, 0
	}
	input = readInt64(usage, "prompt_tokens")
	output = readInt64(usage, "completion_tokens")
	total = readInt64(usage, "total_tokens")
	if total == 0 {
		total = input + output
	}
	return
}

func readInt64(m map[string]interface{}, key string) int64 {
	switch v := m[key].(type) {
	case float64:
		return int64(v)
	case int64:
		return v
	case int:
		return int64(v)
	case json.Number:
		if n, err := v.Int64(); err == nil {
			return n
		}
	}
	return 0
}

func chatOutputFromResult(result map[string]interface{}) string {
	if s, ok := result["output"].(string); ok && s != "" {
		return s
	}
	if choices, ok := result["choices"].([]interface{}); ok && len(choices) > 0 {
		if first, ok := choices[0].(map[string]interface{}); ok {
			if msg, ok := first["message"].(map[string]interface{}); ok {
				if c, ok := msg["content"].(string); ok {
					return c
				}
			}
			if t, ok := first["text"].(string); ok {
				return t
			}
		}
	}
	return ""
}

func applyCapturedPrompt(payload *metering.MeteringPayload, prompt string, output string) {
	if prompt != "" {
		inputMessages, truncated := metering.FormatPromptAsInputMessages(prompt)
		if inputMessages != "" {
			payload.InputMessages = inputMessages
		}
		payload.PromptsTruncated = truncated
	}
	if output != "" {
		payload.OutputResponse = output
	}
}
