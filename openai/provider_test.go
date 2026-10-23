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
			name:     "Cognitive Services host",
			url:      "https://my-resource.cognitiveservices.azure.com",
			expected: true,
		},
		{
			name:     "AI Foundry host",
			url:      "https://my-resource.services.ai.azure.com/models",
			expected: true,
		},
		{
			name:     "API Management host",
			url:      "https://my-gateway.azure-api.net/openai",
			expected: true,
		},
		{
			name:     "US Government cloud host",
			url:      "https://my-resource.openai.azure.us",
			expected: true,
		},
		{
			name:     "China cloud host",
			url:      "https://my-resource.openai.azure.cn",
			expected: true,
		},
		{
			name:     "proxy whose host only mentions azureopenai",
			url:      "https://azureopenai.proxy.example.com/v1",
			expected: false,
		},
		{
			name:     "host that only contains .azure. in the middle",
			url:      "https://test.azure.example.com",
			expected: false,
		},
		{
			name:     "host that ends in azure.com without a dot boundary",
			url:      "https://notazure.com",
			expected: false,
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
		{
			name:     "azure only in the path",
			url:      "https://proxy.example.com/azure.com/v1",
			expected: false,
		},
		{
			name:     "azureopenai only in the query",
			url:      "https://api.openai.com/v1?route=azureopenai",
			expected: false,
		},
		{
			name:     "upper-case Azure host",
			url:      "https://MY-RESOURCE.OPENAI.AZURE.COM/",
			expected: true,
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
