package grok

// Provider represents the AI provider being used
type Provider string

const (
	ProviderXAI Provider = "XAI"
)

// DetectProvider detects which provider is being used based on configuration
func DetectProvider(cfg *Config) Provider {
	// Always xAI for this middleware
	return ProviderXAI
}

// IsXAI returns true if the provider is xAI
func (p Provider) IsXAI() bool {
	return p == ProviderXAI
}

// String returns the string representation of the provider
func (p Provider) String() string {
	return string(p)
}
