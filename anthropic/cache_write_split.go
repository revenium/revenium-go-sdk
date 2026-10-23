package anthropic

import (
	"encoding/json"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/packages/respjson"
	brtypes "github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
)

// Values must be the exact JSON names of anthropic.CacheCreation fields.
var anthropicCacheWriteKeyByConverseTTL = map[brtypes.CacheTTL]string{
	brtypes.CacheTTLFiveMinutes: "ephemeral_5m_input_tokens",
	brtypes.CacheTTLOneHour:     "ephemeral_1h_input_tokens",
}

type cacheWriteSplit struct {
	fiveMinute *int64
	oneHour    *int64
}

type cacheWriteSplitReporter interface {
	reportedCacheWriteSplit() cacheWriteSplit
}

func cacheWriteSplitOf(byTTL anthropic.CacheCreation) cacheWriteSplit {
	return cacheWriteSplit{
		fiveMinute: reportedTokenCount(byTTL.Ephemeral5mInputTokens, byTTL.JSON.Ephemeral5mInputTokens),
		oneHour:    reportedTokenCount(byTTL.Ephemeral1hInputTokens, byTTL.JSON.Ephemeral1hInputTokens),
	}
}

func betaCacheWriteSplitOf(byTTL anthropic.BetaCacheCreation) cacheWriteSplit {
	return cacheWriteSplit{
		fiveMinute: reportedTokenCount(byTTL.Ephemeral5mInputTokens, byTTL.JSON.Ephemeral5mInputTokens),
		oneHour:    reportedTokenCount(byTTL.Ephemeral1hInputTokens, byTTL.JSON.Ephemeral1hInputTokens),
	}
}

func (s cacheWriteSplit) isReported() bool {
	return s.fiveMinute != nil || s.oneHour != nil
}

func reportedTokenCount(count int64, field respjson.Field) *int64 {
	if !field.Valid() {
		return nil
	}
	return &count
}

func cacheWriteSplitInUsage(usageJSON string) cacheWriteSplit {
	var usage struct {
		CacheCreation anthropic.CacheCreation `json:"cache_creation"`
	}
	if json.Unmarshal([]byte(usageJSON), &usage) != nil {
		return cacheWriteSplit{}
	}
	return cacheWriteSplitOf(usage.CacheCreation)
}

func betaCacheCreationInUsage(usageJSON string) anthropic.BetaCacheCreation {
	var usage struct {
		CacheCreation anthropic.BetaCacheCreation `json:"cache_creation"`
	}
	if json.Unmarshal([]byte(usageJSON), &usage) != nil {
		return anthropic.BetaCacheCreation{}
	}
	return usage.CacheCreation
}

func decodeCacheCreation(rawUsage map[string]interface{}) anthropic.CacheCreation {
	var byTTL anthropic.CacheCreation
	nested, ok := rawUsage["cache_creation"].(map[string]interface{})
	if !ok {
		return byTTL
	}
	encoded, err := json.Marshal(nested)
	if err != nil {
		return byTTL
	}
	if err := json.Unmarshal(encoded, &byTTL); err != nil {
		return anthropic.CacheCreation{}
	}
	return byTTL
}

// converseCacheCreation is Converse's per-TTL cache write, kept only when its
// entries all name a TTL Revenium prices and add up to the whole write, since
// a partial split would understate it. It decodes through JSON because
// cacheWriteSplitOf reads the Valid flags that only Unmarshal sets; assigning
// the fields directly would silently drop the split.
func converseCacheCreation(usage *brtypes.TokenUsage) anthropic.CacheCreation {
	if usage == nil || usage.CacheWriteInputTokens == nil || len(usage.CacheDetails) == 0 {
		return anthropic.CacheCreation{}
	}
	byTTL := map[string]int64{}
	var total int64
	for _, detail := range usage.CacheDetails {
		key, known := anthropicCacheWriteKeyByConverseTTL[detail.Ttl]
		if !known || detail.InputTokens == nil {
			return anthropic.CacheCreation{}
		}
		byTTL[key] += int64(*detail.InputTokens)
		total += int64(*detail.InputTokens)
	}
	if total != int64(*usage.CacheWriteInputTokens) {
		return anthropic.CacheCreation{}
	}
	encoded, err := json.Marshal(byTTL)
	if err != nil {
		return anthropic.CacheCreation{}
	}
	var cacheCreation anthropic.CacheCreation
	if err := json.Unmarshal(encoded, &cacheCreation); err != nil {
		return anthropic.CacheCreation{}
	}
	return cacheCreation
}
