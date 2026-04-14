package litellm

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestExtractProvider(t *testing.T) {
	tests := []struct {
		model    string
		expected string
	}{
		{"openai/gpt-4", "OpenAI"},
		{"openai/gpt-4o-mini", "OpenAI"},
		{"anthropic/claude-3-opus-20240229", "Anthropic"},
		{"vertex_ai/gemini-pro", "Google Vertex AI"},
		{"gemini/gemini-1.5-flash", "Google"},
		{"cohere/command-r-plus", "Cohere"},
		{"mistral/mistral-large-latest", "Mistral"},
		{"groq/llama-3.1-70b", "Groq"},
		{"ollama/llama3", "Ollama"},
		{"together_ai/meta-llama/Llama-3.1-8B", "Together AI"},
		// Bare meta-llama (no prefix) must not be misattributed to Together AI
		{"meta-llama/Llama-3.1-8B", "LiteLLM"},
		{"bedrock/anthropic.claude-v2", "AWS Bedrock"},
		{"perplexity/sonar-small", "Perplexity"},
		{"azure/gpt-4", "Azure"},
		// No prefix - resolved via pattern fallback
		{"gpt-4", "OpenAI"},
		{"claude-3-opus", "Anthropic"},
		{"my-gemini-tuned", "Google"},
		// No prefix, no matching pattern
		{"totally-unknown-model", "LiteLLM"},
		// Unknown prefix with no matching pattern
		{"unknown/model", "LiteLLM"},
	}

	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			result := ExtractProvider(tt.model)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestExtractModelSource(t *testing.T) {
	tests := []struct {
		model    string
		expected string
	}{
		{"openai/gpt-4", "OPENAI"},
		{"anthropic/claude-3-opus", "ANTHROPIC"},
		{"vertex_ai/gemini-pro", "GOOGLE"},
		{"cohere/command-r", "COHERE"},
		{"groq/llama-3.1-70b", "GROQ"},
		{"ollama/llama3", "OLLAMA"},
		{"gpt-4", "OPENAI"},
		{"totally-unknown", "LITELLM"},
	}

	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			result := ExtractModelSource(tt.model)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestIsValidModelFormat(t *testing.T) {
	tests := []struct {
		model string
		ok    bool
	}{
		{"openai/gpt-4", true},
		{"anthropic/claude-3.5-sonnet", true},
		{"bedrock/anthropic.claude-v2:1", true},
		{"together_ai/meta-llama/Llama-3.1-8B", true},
		{"", false},
		{"   ", false},
		{"bad space", false},
		{"bad?char", false},
	}
	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			assert.Equal(t, tt.ok, IsValidModelFormat(tt.model))
		})
	}
}

func TestProviderRegistry_ReturnsCopy(t *testing.T) {
	reg := ProviderRegistry()
	assert.NotEmpty(t, reg)
	reg[0].DisplayName = "mutated"
	assert.NotEqual(t, "mutated", ProviderRegistry()[0].DisplayName)
}

func TestProviderRegistry_DeepCopyOfSlices(t *testing.T) {
	reg := ProviderRegistry()
	originalPrefix := reg[0].Prefixes[0]
	reg[0].Prefixes[0] = "injected"
	reg[0].Patterns = append(reg[0].Patterns, "injected")

	fresh := ProviderRegistry()
	assert.Equal(t, originalPrefix, fresh[0].Prefixes[0])
	assert.NotContains(t, fresh[0].Patterns, "injected")
}

func TestExtractProvider_LegacyProvidersPreserved(t *testing.T) {
	tests := map[string]string{
		"anyscale/mistral-7b":      "Anyscale",
		"cloudflare/llama-2-7b":    "Cloudflare",
		"voyage/voyage-2":          "Voyage AI",
		"friendliai/llama-3-70b":   "FriendliAI",
		"friendliai/mixtral-8x7b":  "FriendliAI",
	}
	for model, want := range tests {
		t.Run(model, func(t *testing.T) {
			assert.Equal(t, want, ExtractProvider(model))
		})
	}
}

func TestGetStatus_NilConfigDoesNotPanic(t *testing.T) {
	r := &ReveniumLiteLLM{}
	assert.NotPanics(t, func() {
		s := r.GetStatus()
		assert.False(t, s.Initialized)
		assert.False(t, s.HasConfig)
		assert.Equal(t, "", s.ProxyURL)
	})
}

func TestExtractModelName(t *testing.T) {
	tests := []struct {
		model    string
		expected string
	}{
		{"openai/gpt-4", "gpt-4"},
		{"anthropic/claude-3-opus-20240229", "claude-3-opus-20240229"},
		{"vertex_ai/gemini-pro", "gemini-pro"},
		{"gpt-4", "gpt-4"},
		{"together_ai/meta-llama/Llama-3.1-8B", "meta-llama/Llama-3.1-8B"},
	}

	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			result := ExtractModelName(tt.model)
			assert.Equal(t, tt.expected, result)
		})
	}
}
