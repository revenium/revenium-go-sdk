package fal

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/revenium/revenium-go-sdk/core"
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
		WithModelSource("FAL").
		WithStopReason(chatStopReason(result))

	usage := extractChatUsage(result)
	if usage.reported() {
		b.WithTokens(usage.uncachedInput(model), usage.output, usage.total).
			WithReasoningTokens(usage.reasoning, 0, usage.cacheRead)
	}

	payload := b.Build()
	metering.ApplyMetadata(payload, metadata)

	if capturePrompts {
		metering.ApplyCapturedPrompt(payload, prompt, chatOutputFromResult(result))
	}

	return payload
}

type chatUsage struct {
	input     int64
	output    int64
	total     int64
	cacheRead int64
	reasoning int64
}

func extractChatUsage(result map[string]interface{}) chatUsage {
	usage, ok := result["usage"].(map[string]interface{})
	if !ok {
		return chatUsage{}
	}
	u := chatUsage{
		input:     readInt64(usage, "prompt_tokens"),
		output:    readInt64(usage, "completion_tokens"),
		total:     readInt64(usage, "total_tokens"),
		cacheRead: readInt64(nestedMap(usage, "prompt_tokens_details"), "cached_tokens"),
		reasoning: readInt64(nestedMap(usage, "completion_tokens_details"), "reasoning_tokens"),
	}
	if u.total == 0 {
		u.total = u.input + u.output
	}
	return u
}

func (u chatUsage) reported() bool {
	return u.total > 0 || u.input > 0 || u.output > 0
}

// uncachedInput nets the cache reads out of prompt_tokens, which includes them in fal's
// OpenAI-shaped usage. Revenium removes cached tokens from the input count itself only for
// OPENAI_SHAPED_CACHE_POOL_PROVIDERS on the platform's completion metric processor, which does
// not list fal_ai, so a gross count would bill every cached token at the input rate as well.
func (u chatUsage) uncachedInput(model string) int64 {
	if u.cacheRead > u.input {
		core.Warn("fal reported %d prompt tokens for %s, fewer than its %d cached tokens; metering the prompt count unchanged",
			u.input, model, u.cacheRead)
		return u.input
	}
	return u.input - u.cacheRead
}

func chatStopReason(result map[string]interface{}) string {
	finishReason, _ := firstChoice(result)["finish_reason"].(string)
	switch strings.ToLower(finishReason) {
	case "", "stop":
		return string(core.StopReasonEnd)
	case "length":
		return string(core.StopReasonTokenLimit)
	case "tool_calls", "function_call":
		return string(core.StopReasonEndSequence)
	case "content_filter":
		return string(core.StopReasonError)
	default:
		core.Debug("Unknown fal chat finish_reason %q; recording END", finishReason)
		return string(core.StopReasonEnd)
	}
}

func firstChoice(result map[string]interface{}) map[string]interface{} {
	choices, _ := result["choices"].([]interface{})
	if len(choices) == 0 {
		return nil
	}
	first, _ := choices[0].(map[string]interface{})
	return first
}

func nestedMap(m map[string]interface{}, key string) map[string]interface{} {
	nested, _ := m[key].(map[string]interface{})
	return nested
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
	first := firstChoice(result)
	if c, ok := nestedMap(first, "message")["content"].(string); ok {
		return c
	}
	if t, ok := first["text"].(string); ok {
		return t
	}
	return ""
}
