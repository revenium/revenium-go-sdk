package groq

// Provider represents the AI provider being used
type Provider string

const (
	ProviderGroq Provider = "GROQ"
)

// DetectProvider detects which provider is being used based on configuration
func DetectProvider(cfg *Config) Provider {
	// Always Groq for this middleware
	return ProviderGroq
}

// IsGroq returns true if the provider is Groq
func (p Provider) IsGroq() bool {
	return p == ProviderGroq
}

// String returns the string representation of the provider
func (p Provider) String() string {
	return string(p)
}
