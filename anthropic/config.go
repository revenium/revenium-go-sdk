package anthropic

import (
	"net/http"
	"os"

	"github.com/revenium/revenium-go-sdk/core"
)

type Config struct {
	AnthropicAPIKey string
	BaseURL         string

	Revenium *core.ReveniumConfig

	AWSAccessKeyID     string
	AWSSecretAccessKey string
	AWSSessionToken    string
	AWSRegion          string
	AWSProfile         string
	AWSModelARNBase    string
	BedrockDisabled    bool
	BedrockUseConverse bool

	BedrockFallbackToAnthropic bool

	bedrockFallbackSetInCode bool
	bedrockHTTPClient        *http.Client
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

func WithBedrockUseConverse(enabled bool) Option {
	return func(c *Config) {
		c.BedrockUseConverse = enabled
	}
}

// WithBedrockFallbackToAnthropic re-sends a Bedrock call that fails to the
// Anthropic API, where it is recorded as an Anthropic call. Off by default: a
// failed Bedrock call returns its error.
func WithBedrockFallbackToAnthropic(enabled bool) Option {
	return func(c *Config) {
		c.BedrockFallbackToAnthropic = enabled
		c.bedrockFallbackSetInCode = true
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
		if c.AWSSessionToken == "" {
			c.AWSSessionToken = os.Getenv("AWS_SESSION_TOKEN")
		}
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

	if core.IsEnvFlagSet("REVENIUM_BEDROCK_DISABLE") {
		c.BedrockDisabled = true
	}

	if core.IsEnvFlagSet("REVENIUM_BEDROCK_USE_CONVERSE") {
		c.BedrockUseConverse = true
	}

	if !c.bedrockFallbackSetInCode && !c.BedrockFallbackToAnthropic {
		c.BedrockFallbackToAnthropic = core.IsEnvFlagSet("REVENIUM_BEDROCK_FALLBACK_TO_ANTHROPIC")
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
