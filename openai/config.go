package openai

import (
	"fmt"
	"os"

	"github.com/revenium/revenium-go-sdk/core"
)

// DefaultAzureAPIVersion is the Azure OpenAI API version used on the dated
// /openai/deployments routes when neither AZURE_OPENAI_API_VERSION nor
// WithAzureAPIVersion sets one. A v1 endpoint (ending in /openai/v1) sends no
// api-version unless one is set.
const DefaultAzureAPIVersion = "2024-02-15-preview"

type Config struct {
	OpenAIAPIKey string
	OpenAIOrgID  string
	BaseURL      string

	Revenium *core.ReveniumConfig

	AzureAPIKey     string
	AzureEndpoint   string
	AzureAPIVersion string
	AzureDisabled   bool
	// AzureDeploymentModels maps an Azure deployment name to the model it serves,
	// so image and audio records name a model the price catalog lists.
	AzureDeploymentModels map[string]string
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

func WithAzureDeploymentModels(models map[string]string) Option {
	return func(c *Config) {
		c.AzureDeploymentModels = models
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

	if c.AzureDeploymentModels == nil {
		c.AzureDeploymentModels = parseDeploymentModels(os.Getenv("AZURE_OPENAI_DEPLOYMENT_MODELS"))
	}

	if core.IsEnvFlagSet("REVENIUM_AZURE_DISABLE") {
		c.AzureDisabled = true
	}

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

func (c *Config) azureEndpoint() string {
	if c.AzureEndpoint != "" {
		return c.AzureEndpoint
	}
	return c.BaseURL
}

func (c *Config) effectiveBaseURL() string {
	if c.BaseURL != "" {
		return c.BaseURL
	}
	return os.Getenv("OPENAI_BASE_URL")
}

func (c *Config) callsOpenAIDirectly() bool {
	if c == nil {
		return false
	}
	base := c.effectiveBaseURL()
	return base == "" || urlHost(base) == openAIAPIHost
}

func (c *Config) mediaModel(provider Provider, model string) string {
	if c == nil || !provider.IsAzure() {
		return model
	}
	if catalogModel, ok := c.AzureDeploymentModels[model]; ok {
		return catalogModel
	}
	return model
}

func (c *Config) azureAPIVersion() string {
	if c.AzureAPIVersion != "" {
		return c.AzureAPIVersion
	}
	return DefaultAzureAPIVersion
}

func (c *Config) validateAzure(provider Provider) error {
	if !provider.IsAzure() || c.AzureAPIKey != "" {
		return nil
	}
	return core.NewConfigError(fmt.Sprintf(
		"%s is an Azure OpenAI address but no Azure key is set. With NewReveniumOpenAI, set Config.AzureAPIKey, "+
			"or set Config.AzureDisabled to true to call it as a plain OpenAI-compatible endpoint. With Initialize, "+
			"set AZURE_OPENAI_API_KEY or WithAzureAPIKey, or turn Azure detection off with REVENIUM_AZURE_DISABLE=true "+
			"or WithAzureDisabled(true)",
		c.azureEndpoint()), nil)
}
