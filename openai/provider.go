package openai

import (
	"strings"

	"github.com/revenium/revenium-go-sdk/core"
)

// Provider represents the AI provider being used
type Provider string

const (
	ProviderOpenAI Provider = "OPENAI"
	ProviderAzure  Provider = "AZURE"
)

// DetectProvider detects which provider is being used based on configuration
func DetectProvider(cfg *Config) Provider {
	if cfg == nil {
		return ProviderOpenAI
	}

	if cfg.AzureDisabled {
		core.Debug("Azure OpenAI is explicitly disabled, using OpenAI native API")
		return ProviderOpenAI
	}

	if cfg.AzureAPIKey != "" && cfg.AzureEndpoint != "" {
		core.Debug("Azure OpenAI credentials detected, using Azure OpenAI")
		return ProviderAzure
	}

	if cfg.BaseURL != "" && isAzureURL(cfg.BaseURL) {
		core.Debug("Azure OpenAI URL detected in base URL, using Azure OpenAI")
		return ProviderAzure
	}

	core.Debug("No Azure configuration detected, using OpenAI native API")
	return ProviderOpenAI
}

// isAzureURL checks if a URL is an Azure OpenAI URL
func isAzureURL(url string) bool {
	url = strings.ToLower(url)
	return strings.Contains(url, "azure.com") ||
		strings.Contains(url, "openai.azure.com") ||
		strings.Contains(url, ".azure.") ||
		strings.Contains(url, "azureopenai")
}

// IsOpenAI returns true if the provider is OpenAI
func (p Provider) IsOpenAI() bool {
	return p == ProviderOpenAI
}

// IsAzure returns true if the provider is Azure OpenAI
func (p Provider) IsAzure() bool {
	return p == ProviderAzure
}

// String returns the string representation of the provider
func (p Provider) String() string {
	return string(p)
}

// ModelSource returns the model source for metering
func (p Provider) ModelSource() string {
	return "OPENAI"
}
