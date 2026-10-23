package openai

import (
	"net/url"
	"strings"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/revenium/revenium-go-sdk/core/metering"
)

// Provider represents the AI provider being used
type Provider string

const (
	ProviderOpenAI Provider = "OPENAI"
	ProviderAzure  Provider = "AZURE"
	ProviderXAI    Provider = "XAI"
)

const (
	xaiDomain     = "x.ai"
	openAIAPIHost = "api.openai.com"
)

// DetectProvider detects which provider is being used based on configuration
func DetectProvider(cfg *Config) Provider {
	if cfg == nil {
		return ProviderOpenAI
	}

	if cfg.AzureDisabled {
		core.Debug("Azure OpenAI is explicitly disabled")
	} else if azureConfigured(cfg) {
		return ProviderAzure
	}

	if isXAIURL(cfg.effectiveBaseURL()) {
		core.Debug("xAI base URL detected, recording calls as xAI")
		return ProviderXAI
	}

	core.Debug("No Azure or xAI configuration detected, using OpenAI native API")
	return ProviderOpenAI
}

func azureConfigured(cfg *Config) bool {
	if cfg.AzureAPIKey != "" && cfg.AzureEndpoint != "" {
		core.Debug("Azure OpenAI credentials detected, using Azure OpenAI")
		return true
	}
	if cfg.BaseURL != "" && isAzureURL(cfg.BaseURL) {
		core.Debug("Azure OpenAI URL detected in base URL, using Azure OpenAI")
		return true
	}
	return false
}

func isXAIURL(rawURL string) bool {
	host := urlHost(rawURL)
	return host == xaiDomain || strings.HasSuffix(host, "."+xaiDomain)
}

func urlHost(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return strings.TrimSuffix(strings.ToLower(parsed.Hostname()), ".")
}

var azureDomains = []string{"azure.com", "azure.us", "azure.cn", "azure-api.net"}

func isAzureURL(rawURL string) bool {
	host := urlHost(rawURL)
	for _, domain := range azureDomains {
		if host == domain || strings.HasSuffix(host, "."+domain) {
			return true
		}
	}
	return false
}

// IsOpenAI returns true if the provider is OpenAI
func (p Provider) IsOpenAI() bool {
	return p == ProviderOpenAI
}

// IsAzure returns true if the provider is Azure OpenAI
func (p Provider) IsAzure() bool {
	return p == ProviderAzure
}

// IsXAI returns true if the client calls xAI through its OpenAI-compatible API
func (p Provider) IsXAI() bool {
	return p == ProviderXAI
}

// String returns the string representation of the provider
func (p Provider) String() string {
	return string(p)
}

// MediaProvider is the provider name the platform's image and audio price catalog lists,
// matched exactly; chat records keep String().
func (p Provider) MediaProvider() string {
	switch p {
	case ProviderAzure:
		return "azure"
	case ProviderXAI:
		return "xai"
	default:
		return "openai"
	}
}

// ModelSource names the family of models this provider serves.
func (p Provider) ModelSource() string {
	if p.IsXAI() {
		return "XAI"
	}
	return "OPENAI"
}

func (p Provider) recordedModelSource() string {
	if p.IsXAI() {
		return p.ModelSource()
	}
	return ""
}

func (p Provider) record(operation metering.OperationType, model string) *metering.PayloadBuilder {
	return metering.NewPayload(operation, model, p.String()).WithModelSource(p.recordedModelSource())
}

func (p Provider) mediaRecord(operation metering.OperationType, model string) *metering.PayloadBuilder {
	return metering.NewPayload(operation, model, p.MediaProvider()).WithModelSource(p.recordedModelSource())
}
