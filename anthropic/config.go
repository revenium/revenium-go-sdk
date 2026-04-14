package anthropic

import (
	"os"

	"github.com/revenium/revenium-go-sdk/core"
)

type Config struct {
	AnthropicAPIKey string
	BaseURL         string

	Revenium *core.ReveniumConfig

	AWSAccessKeyID     string
	AWSSecretAccessKey string
	AWSRegion          string
	AWSProfile         string
	AWSModelARNBase    string
	BedrockDisabled    bool
}

type Option func(*Config)

func WithAnthropicAPIKey(key string) Option {
	return func(c *Config) {
		c.AnthropicAPIKey = key
	}
}

func WithReveniumAPIKey(key string) Option {
	return func(c *Config) {
		if c.Revenium == nil {
			c.Revenium = &core.ReveniumConfig{}
		}
		c.Revenium.APIKey = key
	}
}

func WithReveniumBaseURL(url string) Option {
	return func(c *Config) {
		if c.Revenium == nil {
			c.Revenium = &core.ReveniumConfig{}
		}
		c.Revenium.BaseURL = url
	}
}

func WithAWSRegion(region string) Option {
	return func(c *Config) {
		c.AWSRegion = region
	}
}

func WithBedrockDisabled(disabled bool) Option {
	return func(c *Config) {
		c.BedrockDisabled = disabled
	}
}

func (c *Config) loadFromEnv() error {
	core.LoadEnvFiles()

	if c.AnthropicAPIKey == "" {
		c.AnthropicAPIKey = os.Getenv("ANTHROPIC_API_KEY")
	}
	c.Revenium = core.MergeReveniumConfig(c.Revenium, core.LoadReveniumConfig())

	if c.AWSAccessKeyID == "" {
		c.AWSAccessKeyID = os.Getenv("AWS_ACCESS_KEY_ID")
	}
	if c.AWSSecretAccessKey == "" {
		c.AWSSecretAccessKey = os.Getenv("AWS_SECRET_ACCESS_KEY")
	}
	if c.AWSRegion == "" {
		c.AWSRegion = core.GetEnvOrDefault("AWS_REGION", "us-east-1")
	}
	if c.AWSProfile == "" {
		c.AWSProfile = os.Getenv("AWS_PROFILE")
	}
	if c.AWSModelARNBase == "" {
		c.AWSModelARNBase = os.Getenv("AWS_MODEL_ARN_ID")
	}

	if os.Getenv("REVENIUM_BEDROCK_DISABLE") == "1" || os.Getenv("REVENIUM_BEDROCK_DISABLE") == "true" {
		c.BedrockDisabled = true
	}

	core.InitializeLogger()
	core.Debug("Loading configuration from environment variables")
	if c.AnthropicAPIKey != "" {
		core.Debug("Anthropic API key loaded (length: %d)", len(c.AnthropicAPIKey))
	}

	return nil
}

func (c *Config) Validate() error {
	if c.Revenium == nil {
		return core.NewConfigError("Revenium config is required", nil)
	}
	if err := core.ValidateReveniumConfig(c.Revenium); err != nil {
		return err
	}
	core.Debug("Configuration validation passed")
	return nil
}
