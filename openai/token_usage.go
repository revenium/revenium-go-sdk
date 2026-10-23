package openai

import (
	"strconv"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/packages/respjson"
	"github.com/openai/openai-go/v3/responses"
	"github.com/revenium/revenium-go-sdk/core/metering"
)

// tokenUsage holds a call's token counts and reportedCost, the cost the provider states for the call,
// which the platform stores as the total.
type tokenUsage struct {
	prompt       int64
	output       int64
	total        int64
	reasoning    int64
	cacheRead    int64
	cacheWrite   int64
	reportedCost *float64
}

// xAI reports cost as cost_in_usd_ticks, its fixed-point USD unit of 1e-10 dollars
// (https://docs.x.ai/docs/api-reference, chat completions usage); 6009000 ticks is 0.0006009 USD.
const (
	xaiCostField      = "cost_in_usd_ticks"
	usdTicksPerDollar = 1e10
)

// xaiReportedCostUSD parses the raw value of xAI's cost field, because the OpenAI SDK marks every field
// outside its schema invalid and the raw value is all there is to read.
func xaiReportedCostUSD(extraFields map[string]respjson.Field) *float64 {
	field, present := extraFields[xaiCostField]
	if !present {
		return nil
	}
	ticks, err := strconv.ParseInt(field.Raw(), 10, 64)
	if err != nil || ticks < 0 {
		return nil
	}
	cost := float64(ticks) / usdTicksPerDollar
	return &cost
}

func chatTokenUsage(usage openai.CompletionUsage) tokenUsage {
	return tokenUsage{
		prompt:       usage.PromptTokens,
		output:       usage.CompletionTokens,
		total:        usage.TotalTokens,
		reasoning:    usage.CompletionTokensDetails.ReasoningTokens,
		cacheRead:    usage.PromptTokensDetails.CachedTokens,
		cacheWrite:   usage.PromptTokensDetails.CacheWriteTokens,
		reportedCost: xaiReportedCostUSD(usage.JSON.ExtraFields),
	}
}

func responseTokenUsage(usage responses.ResponseUsage) tokenUsage {
	return tokenUsage{
		prompt:       usage.InputTokens,
		output:       usage.OutputTokens,
		total:        usage.TotalTokens,
		reasoning:    usage.OutputTokensDetails.ReasoningTokens,
		cacheRead:    usage.InputTokensDetails.CachedTokens,
		cacheWrite:   usage.InputTokensDetails.CacheWriteTokens,
		reportedCost: xaiReportedCostUSD(usage.JSON.ExtraFields),
	}
}

// The platform removes cache reads from the prompt of OpenAI-shaped records itself, but not cache writes.
func (u tokenUsage) inputTokensExcludingCacheWrites() int64 {
	return max(0, u.prompt-u.cacheWrite)
}

// xAI reasoning models leave reasoning out of completion_tokens and only total_tokens shows it (see grok/usage.go).
func (u tokenUsage) reasoningCountedInOutput() bool {
	return u.total == u.prompt+u.output
}

func (u tokenUsage) billedOutputTokens(provider Provider) int64 {
	if !provider.IsXAI() || u.reasoningCountedInOutput() {
		return u.output
	}
	return u.output + u.reasoning
}

func (u tokenUsage) chatRecord(provider Provider, model string) *metering.PayloadBuilder {
	return provider.record(metering.OperationChat, model).
		WithTokens(u.inputTokensExcludingCacheWrites(), u.billedOutputTokens(provider), u.total).
		WithReasoningTokens(u.reasoning, u.cacheWrite, u.cacheRead).
		WithCosts(nil, nil, nil, nil, u.reportedCost)
}
