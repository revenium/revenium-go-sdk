package openai

import (
	"os"

	"github.com/revenium/revenium-go-sdk/core"
)

type Config struct {
	OpenAIAPIKey string
	OpenAIOrgID  string
	BaseURL      string

	Revenium *core.ReveniumConfig

	AzureAPIKey     string
	AzureEndpoint   string
	AzureAPIVersion string
	AzureDisabled   bool
}

type Option func(*Config)

func WithOpenAIAPIKey(key string) Option {
	return func(c *Config) {
		c.OpenAIAPIKey = key
	}
}

func WithOpenAIOrgID(orgID string) Option {
	return func(c *Config) {
		c.OpenAIOrgID = orgID
	}
}

func WithBaseURL(url string) Option {
	return func(c *Config) {
		c.BaseURL = url
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

func WithAzureAPIKey(key string) Option {
	return func(c *Config) {
		c.AzureAPIKey = key
	}
}

func WithAzureEndpoint(endpoint string) Option {
	return func(c *Config) {
		c.AzureEndpoint = endpoint
	}
}

func WithAzureAPIVersion(version string) Option {
	return func(c *Config) {
		c.AzureAPIVersion = version
	}
}

func WithAzureDisabled(disabled bool) Option {
	return func(c *Config) {
		c.AzureDisabled = disabled
	}
}

func WithDebug(debug bool) Option {
	return func(c *Config) {
		if c.Revenium == nil {
			c.Revenium = &core.ReveniumConfig{}
		}
		c.Revenium.Debug = debug
	}
}

func (c *Config) loadFromEnv() error {
	core.LoadEnvFiles()

	if c.OpenAIAPIKey == "" {
		c.OpenAIAPIKey = os.Getenv("OPENAI_API_KEY")
	}
	if c.OpenAIOrgID == "" {
		c.OpenAIOrgID = os.Getenv("OPENAI_ORG_ID")
	}
	c.Revenium = core.MergeReveniumConfig(c.Revenium, core.LoadReveniumConfig())

	if c.AzureAPIKey == "" {
		c.AzureAPIKey = os.Getenv("AZURE_OPENAI_API_KEY")
	}
	if c.AzureEndpoint == "" {
		c.AzureEndpoint = os.Getenv("AZURE_OPENAI_ENDPOINT")
	}
	if c.AzureAPIVersion == "" {
		c.AzureAPIVersion = os.Getenv("AZURE_OPENAI_API_VERSION")
	}

	if os.Getenv("REVENIUM_AZURE_DISABLE") == "1" || os.Getenv("REVENIUM_AZURE_DISABLE") == "true" {
		c.AzureDisabled = true
	}

	core.SetGlobalDebug(c.Revenium.Debug)
	core.Debug("Loading configuration from environment variables")

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
