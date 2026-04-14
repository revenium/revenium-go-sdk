package ollama

// Provider represents the AI provider being used
type Provider string

const (
	ProviderOllama Provider = "OLLAMA"
)

// DetectProvider detects which provider is being used based on configuration
func DetectProvider(cfg *Config) Provider {
	// Always Ollama for this middleware
	return ProviderOllama
}

// IsOllama returns true if the provider is Ollama
func (p Provider) IsOllama() bool {
	return p == ProviderOllama
}

// String returns the string representation of the provider
func (p Provider) String() string {
	return string(p)
}
