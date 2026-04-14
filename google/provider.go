package google

import (
	"github.com/revenium/revenium-go-sdk/core"
)

// Provider represents the Google AI provider type
type Provider string

const (
	ProviderGoogleAI Provider = "GOOGLE_AI"
	ProviderVertexAI Provider = "VERTEX_AI"
)

// DetectProvider detects the provider based on the configuration
func DetectProvider(cfg *Config) Provider {
	if cfg == nil {
		return ProviderGoogleAI
	}

	// If Vertex is explicitly disabled, use Google AI
	if cfg.VertexDisabled {
		core.Debug("Vertex AI explicitly disabled, using Google AI")
		return ProviderGoogleAI
	}

	// Auto-detect based on configuration
	if cfg.ProjectID != "" {
		core.Debug("Detected Vertex AI provider (ProjectID: %s)", cfg.ProjectID)
		return ProviderVertexAI
	}

	return ProviderGoogleAI
}

// IsGoogleAI returns true if the provider is Google AI
func (p Provider) IsGoogleAI() bool {
	return p == ProviderGoogleAI
}

// IsVertexAI returns true if the provider is Vertex AI
func (p Provider) IsVertexAI() bool {
	return p == ProviderVertexAI
}

// String returns the string representation of the provider
func (p Provider) String() string {
	return string(p)
}
