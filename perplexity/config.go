package perplexity

import (
	"os"

	"github.com/revenium/revenium-go-sdk/core"
)

const defaultPerplexityBaseURL = "https://api.perplexity.ai"

type Config struct {
	PerplexityAPIKey  string
	PerplexityBaseURL string

	Revenium *core.ReveniumConfig
}

type Option func(*Config)

func WithPerplexityAPIKey(key string) Option {
	return func(c *Config) { c.PerplexityAPIKey = key }
}

func WithPerplexityBaseURL(url string) Option {
	return func(c *Config) { c.PerplexityBaseURL = url }
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

	if c.PerplexityAPIKey == "" {
		c.PerplexityAPIKey = os.Getenv("PERPLEXITY_API_KEY")
	}
	if c.PerplexityBaseURL == "" {
		perplexityBase := os.Getenv("PERPLEXITY_API_BASE_URL")
		if perplexityBase == "" {
			perplexityBase = defaultPerplexityBaseURL
		}
		c.PerplexityBaseURL = perplexityBase
	}

	programmaticBaseURL := ""
	if c.Revenium != nil {
		programmaticBaseURL = c.Revenium.BaseURL
	}

	c.Revenium = core.MergeReveniumConfig(c.Revenium, core.LoadReveniumConfig())

	if c.Revenium.APIKey == "" {
		c.Revenium.APIKey = os.Getenv("REVENIUM_API_KEY")
	}
	if programmaticBaseURL == "" && os.Getenv(core.EnvBaseURL) == "" {
		altBase := os.Getenv("REVENIUM_BASE_URL")
		if altBase != "" {
			c.Revenium.BaseURL = core.NormalizeReveniumBaseURL(altBase)
		}
	}

	core.SetGlobalDebug(c.Revenium.Debug)
	core.Debug("Configuration loaded from environment")
	return nil
}

func (c *Config) Validate() error {
	if c.Revenium == nil {
		return core.NewConfigError("Revenium config is required", nil)
	}
	if err := core.ValidateReveniumConfig(c.Revenium); err != nil {
		return err
	}
	if c.PerplexityAPIKey == "" {
		return core.NewConfigError("PERPLEXITY_API_KEY is required", nil)
	}
	if len(c.PerplexityAPIKey) < 5 || c.PerplexityAPIKey[:5] != "pplx-" {
		return core.NewConfigError("invalid Perplexity API key format (expected prefix 'pplx-')", nil)
	}
	return nil
}
