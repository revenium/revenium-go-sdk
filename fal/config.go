package fal

import (
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/revenium/revenium-go-sdk/core"
)

type Config struct {
	FalAPIKey      string
	FalBaseURL     string
	QueueBaseURL   string
	RequestTimeout time.Duration

	Revenium *core.ReveniumConfig

	CapturePrompts    bool
	capturePromptsSet bool
}

type Option func(*Config)

func WithFalAPIKey(key string) Option {
	return func(c *Config) {
		c.FalAPIKey = key
	}
}

func WithRequestTimeout(timeout time.Duration) Option {
	return func(c *Config) {
		c.RequestTimeout = timeout
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

func WithReveniumOrgID(id string) Option {
	return func(c *Config) {
		if c.Revenium == nil {
			c.Revenium = &core.ReveniumConfig{}
		}
		c.Revenium.OrgID = id
	}
}

func WithReveniumProductID(id string) Option {
	return func(c *Config) {
		if c.Revenium == nil {
			c.Revenium = &core.ReveniumConfig{}
		}
		c.Revenium.ProductID = id
	}
}

func WithCapturePrompts(capture bool) Option {
	return func(c *Config) {
		c.CapturePrompts = capture
		c.capturePromptsSet = true
	}
}

func (c *Config) loadFromEnv() error {
	core.LoadEnvFiles()

	if c.FalAPIKey == "" {
		c.FalAPIKey = os.Getenv("FAL_API_KEY")
	}
	if c.FalAPIKey == "" {
		c.FalAPIKey = os.Getenv("FAL_KEY")
	}
	if c.FalBaseURL == "" {
		c.FalBaseURL = core.GetEnvOrDefault("FAL_BASE_URL", "https://fal.run")
	}
	if c.QueueBaseURL == "" {
		c.QueueBaseURL = core.GetEnvOrDefault("FAL_QUEUE_BASE_URL", defaultQueueBaseURL)
	}
	if c.RequestTimeout == 0 {
		c.RequestTimeout = parseDurationFromEnv("FAL_REQUEST_TIMEOUT", 1800*time.Second)
	}

	programmaticOrgID := ""
	programmaticProductID := ""
	if c.Revenium != nil {
		programmaticOrgID = c.Revenium.OrgID
		programmaticProductID = c.Revenium.ProductID
	}

	c.Revenium = core.MergeReveniumConfig(c.Revenium, core.LoadReveniumConfig())

	if programmaticOrgID == "" {
		if name := os.Getenv("REVENIUM_ORGANIZATION_NAME"); name != "" {
			c.Revenium.OrgID = name
		}
	}
	if programmaticProductID == "" {
		if name := os.Getenv("REVENIUM_PRODUCT_NAME"); name != "" {
			c.Revenium.ProductID = name
		}
	}

	if !c.capturePromptsSet {
		c.CapturePrompts = os.Getenv("REVENIUM_CAPTURE_PROMPTS") == "true" || os.Getenv("REVENIUM_CAPTURE_PROMPTS") == "1"
	}

	core.InitializeLogger()

	return nil
}

func (c *Config) Validate() error {
	if c.FalAPIKey == "" {
		return core.NewConfigError("FAL_API_KEY (or FAL_KEY) is required", nil)
	}
	if c.Revenium == nil {
		return core.NewConfigError("Revenium config is required", nil)
	}
	return core.ValidateReveniumConfig(c.Revenium)
}

func parseDurationFromEnv(envKey string, defaultValue time.Duration) time.Duration {
	value := os.Getenv(envKey)
	if value == "" {
		return defaultValue
	}

	if d, err := time.ParseDuration(value); err == nil {
		return d
	}

	value = strings.TrimSpace(value)
	if seconds, err := strconv.ParseInt(value, 10, 64); err == nil {
		return time.Duration(seconds) * time.Second
	}

	return defaultValue
}
