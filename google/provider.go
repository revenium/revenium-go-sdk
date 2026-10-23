package google

import (
	"strings"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/revenium/revenium-go-sdk/core/metering"
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

// mediaIdentity names an Imagen or Veo record the way the platform's media price catalog
// lists it. The platform retries a bare model as "<provider>/<model>", which finds the
// Gemini entries but not the Vertex ones, filed as "vertex_ai/<model>".
func (p Provider) mediaIdentity(op metering.OperationType, model string) (provider, catalogModel string) {
	if !p.IsVertexAI() {
		return "gemini", model
	}
	const vertexModelPrefix = "vertex_ai/"
	if !strings.HasPrefix(model, vertexModelPrefix) {
		model = vertexModelPrefix + model
	}
	if op == metering.OperationVideo {
		return "vertex_ai-video-models", model
	}
	return "vertex_ai-image-models", model
}
