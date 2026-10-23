package litellm

import (
	"strings"

	"github.com/revenium/revenium-go-sdk/core"
)

const claudeModelMarker = "claude"

// Revenium subtracts cached tokens from the input count itself only for
// OpenAI-shaped models (AICompletionMetricProcessor.OPENAI_SHAPED_CACHE_POOL_PROVIDERS
// on the platform) and adds the pools for Claude, so a Claude prompt count from
// LiteLLM, which already contains the cache reads and writes, must be netted here.
func cacheOverlapLeftToClient(model string) bool {
	return strings.Contains(strings.ToLower(model), claudeModelMarker)
}

func billableInputTokens(model string, promptTokens, cacheReadTokens, cacheCreationTokens int64) int64 {
	if promptTokens == 0 || !cacheOverlapLeftToClient(model) {
		return promptTokens
	}
	billable := promptTokens - cacheReadTokens - cacheCreationTokens
	if billable < 0 {
		core.Warn("LiteLLM reported %d prompt tokens for %s, fewer than its %d cache-read and %d cache-creation tokens; metering the prompt count unchanged",
			promptTokens, model, cacheReadTokens, cacheCreationTokens)
		return promptTokens
	}
	return billable
}
