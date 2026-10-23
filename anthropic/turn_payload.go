package anthropic

import (
	"time"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/revenium/revenium-go-sdk/core/metering"
)

type turnUsage struct {
	model               string
	stopReason          string
	inputTokens         int64
	outputTokens        int64
	thinkingTokens      int64
	cacheCreationTokens int64
	cacheReadTokens     int64
	cacheWrites         cacheWriteSplit
	passes              []billedPass
}

type turnTiming struct {
	start          time.Time
	duration       time.Duration
	streamed       bool
	firstTokenTime *time.Time
}

func messageTurnUsage(resp *anthropic.Message) turnUsage {
	passes, _ := billedPassesInUsage(resp.Usage.RawJSON())
	return turnUsage{
		model:               string(resp.Model),
		stopReason:          string(resp.StopReason),
		inputTokens:         resp.Usage.InputTokens,
		outputTokens:        resp.Usage.OutputTokens,
		thinkingTokens:      resp.Usage.OutputTokensDetails.ThinkingTokens,
		cacheCreationTokens: resp.Usage.CacheCreationInputTokens,
		cacheReadTokens:     resp.Usage.CacheReadInputTokens,
		cacheWrites:         cacheWriteSplitOf(resp.Usage.CacheCreation),
		passes:              passes,
	}
}

func betaMessageTurnUsage(resp *anthropic.BetaMessage) turnUsage {
	return turnUsage{
		model:               string(resp.Model),
		stopReason:          string(resp.StopReason),
		inputTokens:         resp.Usage.InputTokens,
		outputTokens:        resp.Usage.OutputTokens,
		thinkingTokens:      resp.Usage.OutputTokensDetails.ThinkingTokens,
		cacheCreationTokens: resp.Usage.CacheCreationInputTokens,
		cacheReadTokens:     resp.Usage.CacheReadInputTokens,
		cacheWrites:         betaCacheWriteSplitOf(resp.Usage.CacheCreation),
		passes:              billedPassesOf(resp.Usage.Iterations),
	}
}

func buildTurnPayload(turn turnUsage, timing turnTiming, provider string, hasVision bool, md map[string]interface{}) *metering.MeteringPayload {
	payload := turnRecord(turn, timing, provider, hasVision)
	metering.ApplyMetadata(payload, md)
	return payload
}

func turnRecord(turn turnUsage, timing turnTiming, provider string, hasVision bool) *metering.MeteringPayload {
	stopReason := "END"
	if turn.stopReason != "" {
		stopReason = MapStopReasonToRevenium(turn.stopReason)
	}

	builder := metering.NewPayload(metering.OperationChat, turn.model, NormalizeProviderName(provider)).
		WithTiming(timing.start, timing.duration).
		WithTokens(turn.inputTokens, turn.outputTokens, turn.inputTokens+turn.outputTokens).
		WithReasoningTokens(turn.thinkingTokens, turn.cacheCreationTokens, turn.cacheReadTokens).
		WithCacheCreationByTTL(turn.cacheWrites.fiveMinute, turn.cacheWrites.oneHour).
		WithStopReason(stopReason)
	if timing.streamed {
		builder = builder.WithStreaming(true, timeToFirstToken(timing), timing.firstTokenTime)
	}
	payload := builder.Build()

	if hasVision {
		payload.Attributes = map[string]interface{}{"hasVision": true}
	}
	return payload
}

func timeToFirstToken(timing turnTiming) int64 {
	if timing.firstTokenTime == nil {
		return 0
	}
	return timing.firstTokenTime.Sub(timing.start).Milliseconds()
}
