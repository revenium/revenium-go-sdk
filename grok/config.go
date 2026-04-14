package grok

import (
	"os"

	"github.com/revenium/revenium-go-sdk/core"
)

type Config struct {
	XAIAPIKey string
	BaseURL   string

	Revenium *core.ReveniumConfig
}

type Option func(*Config)

func WithXAIAPIKey(key string) Option {
	return func(c *Config) {
		c.XAIAPIKey = key
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

func (c *Config) loadFromEnv() error {
	core.LoadEnvFiles()

	if c.XAIAPIKey == "" {
		c.XAIAPIKey = os.Getenv("XAI_API_KEY")
	}
	if c.BaseURL == "" {
		c.BaseURL = core.GetEnvOrDefault("XAI_BASE_URL", "https://api.x.ai/v1")
	}
	c.Revenium = core.MergeReveniumConfig(c.Revenium, core.LoadReveniumConfig())

	core.InitializeLogger()
	core.Debug("Loading configuration from environment variables")
	if c.XAIAPIKey != "" {
		core.Debug("xAI API key loaded (length: %d)", len(c.XAIAPIKey))
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
