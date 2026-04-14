package google

import (
	"os"

	"github.com/revenium/revenium-go-sdk/core"
)

type Config struct {
	GoogleAPIKey string

	ProjectID string
	Location  string

	Revenium *core.ReveniumConfig

	VertexDisabled bool
}

type Option func(*Config)

func WithGoogleAPIKey(key string) Option {
	return func(c *Config) {
		c.GoogleAPIKey = key
	}
}

func WithProjectID(projectID string) Option {
	return func(c *Config) {
		c.ProjectID = projectID
	}
}

func WithLocation(location string) Option {
	return func(c *Config) {
		c.Location = location
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

	if c.GoogleAPIKey == "" {
		c.GoogleAPIKey = os.Getenv("GOOGLE_API_KEY")
	}
	if c.ProjectID == "" {
		c.ProjectID = os.Getenv("GOOGLE_CLOUD_PROJECT")
	}
	if c.Location == "" {
		c.Location = os.Getenv("GOOGLE_CLOUD_LOCATION")
	}

	c.Revenium = core.MergeReveniumConfig(c.Revenium, core.LoadReveniumConfig())

	if os.Getenv("REVENIUM_VERTEX_DISABLE") == "1" || os.Getenv("REVENIUM_VERTEX_DISABLE") == "true" {
		c.VertexDisabled = true
	}

	core.InitializeLogger()
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
