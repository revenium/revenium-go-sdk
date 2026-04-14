package litellm

import (
	"regexp"
	"strings"
)

var providerRegistry = []ProviderPattern{
	{
		Source:      "OPENAI",
		DisplayName: "OpenAI",
		Prefixes:    []string{"openai", "text-completion-openai"},
		Patterns:    []string{"gpt-", "davinci", "curie", "babbage", "text-embedding"},
	},
	{
		Source:      "OPENAI",
		DisplayName: "Azure",
		Prefixes:    []string{"azure"},
		Patterns:    []string{"azure"},
	},
	{
		Source:      "ANTHROPIC",
		DisplayName: "Anthropic",
		Prefixes:    []string{"anthropic"},
		Patterns:    []string{"claude", "anthropic"},
	},
	{
		Source:      "GOOGLE",
		DisplayName: "Google Vertex AI",
		Prefixes:    []string{"vertex_ai"},
		Patterns:    []string{"bison", "gecko"},
	},
	{
		Source:      "GOOGLE",
		DisplayName: "Google",
		Prefixes:    []string{"gemini", "palm"},
		Patterns:    []string{"gemini", "palm"},
	},
	{
		Source:      "COHERE",
		DisplayName: "Cohere",
		Prefixes:    []string{"cohere"},
		Patterns:    []string{"command", "cohere"},
	},
	{
		Source:      "MISTRAL",
		DisplayName: "Mistral",
		Prefixes:    []string{"mistral"},
		Patterns:    []string{"mistral", "mixtral"},
	},
	{
		Source:      "GROQ",
		DisplayName: "Groq",
		Prefixes:    []string{"groq"},
		Patterns:    []string{"groq"},
	},
	{
		Source:      "OLLAMA",
		DisplayName: "Ollama",
		Prefixes:    []string{"ollama"},
		Patterns:    []string{"ollama"},
	},
	{
		Source:      "TOGETHER",
		DisplayName: "Together AI",
		Prefixes:    []string{"together_ai"},
	},
	{
		Source:      "FIREWORKS",
		DisplayName: "Fireworks AI",
		Prefixes:    []string{"fireworks_ai"},
	},
	{
		Source:      "DEEPINFRA",
		DisplayName: "DeepInfra",
		Prefixes:    []string{"deepinfra"},
	},
	{
		Source:      "PERPLEXITY",
		DisplayName: "Perplexity",
		Prefixes:    []string{"perplexity"},
		Patterns:    []string{"sonar", "perplexity"},
	},
	{
		Source:      "ANYSCALE",
		DisplayName: "Anyscale",
		Prefixes:    []string{"anyscale"},
	},
	{
		Source:      "CLOUDFLARE",
		DisplayName: "Cloudflare",
		Prefixes:    []string{"cloudflare"},
	},
	{
		Source:      "VOYAGE",
		DisplayName: "Voyage AI",
		Prefixes:    []string{"voyage"},
	},
	{
		Source:      "FRIENDLIAI",
		DisplayName: "FriendliAI",
		Prefixes:    []string{"friendliai"},
	},
	{
		Source:      "BEDROCK",
		DisplayName: "AWS Bedrock",
		Prefixes:    []string{"bedrock"},
	},
	{
		Source:      "SAGEMAKER",
		DisplayName: "AWS Sagemaker",
		Prefixes:    []string{"sagemaker"},
	},
	{
		Source:      "HUGGINGFACE",
		DisplayName: "Hugging Face",
		Prefixes:    []string{"huggingface"},
		Patterns:    []string{"huggingface"},
	},
	{
		Source:      "AI21",
		DisplayName: "AI21",
		Prefixes:    []string{"ai21"},
	},
	{
		Source:      "REPLICATE",
		DisplayName: "Replicate",
		Prefixes:    []string{"replicate"},
	},
	{
		Source:      "DATABRICKS",
		DisplayName: "Databricks",
		Prefixes:    []string{"databricks"},
	},
}

var validModelFormatRegex = regexp.MustCompile(`^[a-zA-Z0-9/_.:\-]+$`)

// ProviderRegistry returns a deep copy of the registered provider detection rules
func ProviderRegistry() []ProviderPattern {
	out := make([]ProviderPattern, len(providerRegistry))
	for i, p := range providerRegistry {
		out[i] = ProviderPattern{
			Source:      p.Source,
			DisplayName: p.DisplayName,
			Prefixes:    append([]string(nil), p.Prefixes...),
			Patterns:    append([]string(nil), p.Patterns...),
		}
	}
	return out
}

// ExtractProvider returns the display name for the provider inferred from a LiteLLM model string
func ExtractProvider(model string) string {
	if p := matchByPrefix(model); p != nil {
		return p.DisplayName
	}
	if p := matchByPattern(model); p != nil {
		return p.DisplayName
	}
	return "LiteLLM"
}

// ExtractModelSource returns the Revenium model source identifier for a LiteLLM model string
func ExtractModelSource(model string) string {
	if p := matchByPrefix(model); p != nil {
		return p.Source
	}
	if p := matchByPattern(model); p != nil {
		return p.Source
	}
	return "LITELLM"
}

// ExtractModelName returns the model name stripped of any provider prefix
func ExtractModelName(model string) string {
	if idx := strings.Index(model, "/"); idx >= 0 {
		return model[idx+1:]
	}
	return model
}

// IsValidModelFormat validates that a model identifier uses only allowed characters
func IsValidModelFormat(model string) bool {
	trimmed := strings.TrimSpace(model)
	if trimmed == "" {
		return false
	}
	return validModelFormatRegex.MatchString(trimmed)
}

func extractPrefix(model string) string {
	if idx := strings.Index(model, "/"); idx > 0 {
		return strings.ToLower(model[:idx])
	}
	return ""
}

func matchByPrefix(model string) *ProviderPattern {
	prefix := extractPrefix(model)
	if prefix == "" {
		return nil
	}
	for i := range providerRegistry {
		for _, p := range providerRegistry[i].Prefixes {
			if p == prefix {
				return &providerRegistry[i]
			}
		}
	}
	return nil
}

func matchByPattern(model string) *ProviderPattern {
	lower := strings.ToLower(model)
	for i := range providerRegistry {
		for _, pat := range providerRegistry[i].Patterns {
			if pat != "" && strings.Contains(lower, pat) {
				return &providerRegistry[i]
			}
		}
	}
	return nil
}
