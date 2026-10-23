package grok

// xAI reports cost as cost_in_usd_ticks, its fixed-point USD unit of 1e-10 dollars
// (https://docs.x.ai/docs/api-reference, chat completions usage); 6009000 ticks is 0.0006009 USD.
const usdTicksPerDollar = 1e10

func meteredModel(servedModel, requestedModel string) string {
	if servedModel != "" {
		return servedModel
	}
	return requestedModel
}

func (u Usage) reasoningTokens() int64 {
	if u.CompletionTokensDetails == nil {
		return 0
	}
	return int64(u.CompletionTokensDetails.ReasoningTokens)
}

func (u Usage) cacheReadTokens() int64 {
	if u.PromptTokensDetails == nil {
		return 0
	}
	return int64(u.PromptTokensDetails.CachedTokens)
}

// reasoningCountedInCompletion tells the two shapes xAI's usage block takes apart.
// Reasoning models report completion_tokens as answer tokens only and total_tokens
// as prompt + completion + reasoning, so the reasoning count has to be added to the
// billed output; OpenAI-shaped responses fold reasoning into completion_tokens, and
// then total_tokens equals prompt_tokens + completion_tokens exactly. The identity
// is the only signal the block carries, so it is what decides.
func (u Usage) reasoningCountedInCompletion() bool {
	return u.TotalTokens == u.PromptTokens+u.CompletionTokens
}

func (u Usage) billedOutputTokens() int64 {
	completion := int64(u.CompletionTokens)
	if u.reasoningCountedInCompletion() {
		return completion
	}
	return completion + u.reasoningTokens()
}

func (u Usage) reportedCostUSD() *float64 {
	if u.CostInUSDTicks == nil || *u.CostInUSDTicks < 0 {
		return nil
	}
	cost := float64(*u.CostInUSDTicks) / usdTicksPerDollar
	return &cost
}
