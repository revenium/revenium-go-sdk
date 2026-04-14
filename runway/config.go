package runway

import (
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/revenium/revenium-go-sdk/core"
)

const DefaultRequestTimeout = 1800 * time.Second

type Config struct {
	RunwayAPIKey   string
	RunwayBaseURL  string
	RunwayVersion  string
	RequestTimeout time.Duration

	Revenium *core.ReveniumConfig

	CapturePrompts    bool
	capturePromptsSet bool
}

type Option func(*Config)

func WithRunwayAPIKey(key string) Option {
	return func(c *Config) {
		c.RunwayAPIKey = key
	}
}

func WithRunwayBaseURL(url string) Option {
	return func(c *Config) {
		c.RunwayBaseURL = url
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

func WithRequestTimeout(timeout time.Duration) Option {
	return func(c *Config) {
		c.RequestTimeout = timeout
	}
}

func WithCapturePrompts(capture bool) Option {
	return func(c *Config) {
		c.CapturePrompts = capture
		c.capturePromptsSet = true
	}
}

func (c *Config) LoadFromEnv() error {
	core.LoadEnvFiles()

	if c.RunwayAPIKey == "" {
		c.RunwayAPIKey = os.Getenv("RUNWAY_API_KEY")
	}
	if c.RunwayBaseURL == "" {
		c.RunwayBaseURL = core.GetEnvOrDefault("RUNWAY_BASE_URL", "https://api.dev.runwayml.com")
	}
	if c.RunwayVersion == "" {
		c.RunwayVersion = core.GetEnvOrDefault("RUNWAY_VERSION", "2024-11-06")
	}
	if c.RequestTimeout == 0 {
		c.RequestTimeout = parseDurationFromEnv("RUNWAY_REQUEST_TIMEOUT", DefaultRequestTimeout)
	}

	c.Revenium = core.MergeReveniumConfig(c.Revenium, core.LoadReveniumConfig())

	if !c.capturePromptsSet {
		c.CapturePrompts = os.Getenv("REVENIUM_CAPTURE_PROMPTS") == "true" || os.Getenv("REVENIUM_CAPTURE_PROMPTS") == "1"
	}

	core.InitializeLogger()
	core.Debug("Loading configuration from environment variables")
	if c.RunwayAPIKey != "" {
		core.Debug("Runway API key loaded (length: %d)", len(c.RunwayAPIKey))
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
	if c.RunwayAPIKey == "" {
		return core.NewConfigError("RUNWAY_API_KEY is required", nil)
	}
	core.Debug("Configuration validation passed")
	return nil
}

func parseDurationFromEnv(key string, defaultValue time.Duration) time.Duration {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}

	if d, err := time.ParseDuration(value); err == nil {
		return d
	}

	value = strings.TrimSpace(value)
	if seconds, err := strconv.ParseFloat(value, 64); err == nil {
		return time.Duration(seconds * float64(time.Second))
	}

	return defaultValue
}
