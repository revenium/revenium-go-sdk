package openai

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDetectProvider(t *testing.T) {
	tests := []struct {
		name     string
		config   *Config
		expected Provider
	}{
		{
			name:     "nil config",
			config:   nil,
			expected: ProviderOpenAI,
		},
		{
			name: "Azure disabled explicitly",
			config: &Config{
				AzureDisabled: true,
				AzureAPIKey:   "test-key",
				AzureEndpoint: "https://test.openai.azure.com",
			},
			expected: ProviderOpenAI,
		},
		{
			name: "Azure credentials present",
			config: &Config{
				AzureAPIKey:   "test-key",
				AzureEndpoint: "https://test.openai.azure.com",
			},
			expected: ProviderAzure,
		},
		{
			name: "Azure URL in base URL",
			config: &Config{
				BaseURL: "https://test.openai.azure.com",
			},
			expected: ProviderAzure,
		},
		{
			name: "OpenAI by default",
			config: &Config{
				OpenAIAPIKey: "sk-test",
			},
			expected: ProviderOpenAI,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := DetectProvider(tt.config)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestIsAzureURL(t *testing.T) {
	tests := []struct {
		name     string
		url      string
		expected bool
	}{
		{
			name:     "azure.com",
			url:      "https://test.azure.com",
			expected: true,
		},
		{
			name:     "openai.azure.com",
			url:      "https://test.openai.azure.com",
			expected: true,
		},
		{
			name:     "contains .azure.",
			url:      "https://test.azure.example.com",
			expected: true,
		},
		{
			name:     "contains azureopenai",
			url:      "https://azureopenai.example.com",
			expected: true,
		},
		{
			name:     "OpenAI URL",
			url:      "https://api.openai.com",
			expected: false,
		},
		{
			name:     "custom URL",
			url:      "https://api.example.com",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isAzureURL(tt.url)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestProviderMethods(t *testing.T) {
	t.Run("IsOpenAI", func(t *testing.T) {
		assert.True(t, ProviderOpenAI.IsOpenAI())
		assert.False(t, ProviderAzure.IsOpenAI())
	})

	t.Run("IsAzure", func(t *testing.T) {
		assert.True(t, ProviderAzure.IsAzure())
		assert.False(t, ProviderOpenAI.IsAzure())
	})

	t.Run("String", func(t *testing.T) {
		assert.Equal(t, "OPENAI", ProviderOpenAI.String())
		assert.Equal(t, "AZURE", ProviderAzure.String())
	})

	t.Run("ModelSource", func(t *testing.T) {
		assert.Equal(t, "OPENAI", ProviderOpenAI.ModelSource())
		assert.Equal(t, "OPENAI", ProviderAzure.ModelSource())
	})
}
