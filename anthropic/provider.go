package anthropic

import (
	"strings"

	"github.com/revenium/revenium-go-sdk/core"
)

type Provider string

const (
	ProviderAnthropic Provider = "ANTHROPIC"
	ProviderBedrock   Provider = "AWS"
)

func DetectProvider(cfg *Config) Provider {
	if cfg == nil {
		return ProviderAnthropic
	}

	if cfg.BedrockDisabled {
		return ProviderAnthropic
	}

	if cfg.AWSAccessKeyID != "" && cfg.AWSSecretAccessKey != "" {
		return ProviderBedrock
	}

	if cfg.BaseURL != "" && strings.Contains(cfg.BaseURL, "amazonaws.com") {
		return ProviderBedrock
	}

	core.Debug("No explicit provider detected, defaulting to Anthropic")
	return ProviderAnthropic
}

func (p Provider) IsAnthropic() bool {
	return p == ProviderAnthropic
}

func (p Provider) IsBedrock() bool {
	return p == ProviderBedrock
}

func (p Provider) String() string {
	return string(p)
}
