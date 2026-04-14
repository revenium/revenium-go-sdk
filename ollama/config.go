package ollama

import (
	"github.com/revenium/revenium-go-sdk/core"
)

type Config struct {
	BaseURL string

	Revenium *core.ReveniumConfig
}

type Option func(*Config)

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

func (c *Config) loadFromEnv() error {
	core.LoadEnvFiles()

	if c.BaseURL == "" {
		c.BaseURL = core.GetEnvOrDefault("OLLAMA_BASE_URL", "http://localhost:11434/v1")
	}
	c.Revenium = core.MergeReveniumConfig(c.Revenium, core.LoadReveniumConfig())

	core.InitializeLogger()
	core.Debug("Loading configuration from environment variables")
	core.Debug("Ollama base URL: %s", c.BaseURL)

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
