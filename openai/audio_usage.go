package openai

import (
	"encoding/json"
	"sync"

	"github.com/openai/openai-go/v3"
	"github.com/revenium/revenium-go-sdk/core"
	"github.com/revenium/revenium-go-sdk/core/metering"
)

var missingAudioQuantityWarning sync.Once

type audioUsage struct {
	Duration float64 `json:"duration"`
	Usage    struct {
		Seconds            float64 `json:"seconds"`
		InputTokens        int64   `json:"input_tokens"`
		InputTokensDetails struct {
			AudioTokens int64 `json:"audio_tokens"`
		} `json:"input_token_details"`
		OutputTokens int64 `json:"output_tokens"`
		TotalTokens  int64 `json:"total_tokens"`
	} `json:"usage"`
}

func parseAudioUsage(rawJSON string) audioUsage {
	var usage audioUsage
	_ = json.Unmarshal([]byte(rawJSON), &usage)
	return usage
}

func (u audioUsage) seconds() float64 {
	if u.Usage.Seconds > 0 {
		return u.Usage.Seconds
	}
	return u.Duration
}

func (u audioUsage) hasTokens() bool {
	return u.Usage.InputTokens > 0 || u.Usage.OutputTokens > 0
}

func (u audioUsage) hasQuantity() bool {
	return u.hasTokens() || u.seconds() > 0
}

func (u audioUsage) totalTokens() int64 {
	if u.Usage.TotalTokens > 0 {
		return u.Usage.TotalTokens
	}
	return u.Usage.InputTokens + u.Usage.OutputTokens
}

func (u audioUsage) inputAudioTokens() int64 {
	if u.Usage.InputTokensDetails.AudioTokens > 0 {
		return u.Usage.InputTokensDetails.AudioTokens
	}
	return u.Usage.InputTokens
}

func (u audioUsage) applyTo(builder *metering.PayloadBuilder, model, subtype, responseFormat string) *metering.PayloadBuilder {
	if u.hasTokens() {
		builder = builder.
			WithBillingUnit(metering.BillingPerToken).
			WithTokens(u.Usage.InputTokens, u.Usage.OutputTokens, u.totalTokens()).
			WithInputAudioTokens(u.inputAudioTokens())
		if seconds := u.seconds(); seconds > 0 {
			builder = builder.WithAudioDuration(seconds)
		}
	} else {
		builder = withRequiredAudioLength(builder.WithBillingUnit(metering.BillingPerSecond), u.seconds())
	}
	if !u.hasQuantity() {
		missingAudioQuantityWarning.Do(func() {
			core.Warn("OpenAI returned no audio duration or token usage for %s with model %s and response format %q; this call is recorded without a billable quantity. Use the json or verbose_json response format to have it priced.", subtype, model, responseFormat)
		})
	}
	return builder
}

func withRequiredAudioLength(builder *metering.PayloadBuilder, seconds float64) *metering.PayloadBuilder {
	return builder.WithAudioDuration(seconds)
}

func withDurationReporting(params openai.AudioTranslationNewParams) openai.AudioTranslationNewParams {
	if params.ResponseFormat == "" || params.ResponseFormat == openai.AudioTranslationNewParamsResponseFormatJSON {
		params.ResponseFormat = openai.AudioTranslationNewParamsResponseFormatVerboseJSON
	}
	return params
}
