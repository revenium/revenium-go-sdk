package litellm

import (
	"os"

	"github.com/revenium/revenium-go-sdk/core"
)

type Config struct {
	LiteLLMProxyURL string
	LiteLLMAPIKey   string

	Revenium *core.ReveniumConfig
}

type Option func(*Config)

func WithLiteLLMProxyURL(url string) Option {
	return func(c *Config) {
		c.LiteLLMProxyURL = url
	}
}

func WithLiteLLMAPIKey(key string) Option {
	return func(c *Config) {
		c.LiteLLMAPIKey = key
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

	if c.LiteLLMProxyURL == "" {
		c.LiteLLMProxyURL = os.Getenv("LITELLM_PROXY_URL")
	}
	if c.LiteLLMAPIKey == "" {
		c.LiteLLMAPIKey = os.Getenv("LITELLM_API_KEY")
	}

	c.Revenium = core.MergeReveniumConfig(c.Revenium, core.LoadReveniumConfig())

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
	if c.LiteLLMProxyURL == "" {
		return core.NewConfigError("LITELLM_PROXY_URL is required", nil)
	}
	core.Debug("Configuration validation passed")
	return nil
}
