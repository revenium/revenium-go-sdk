package openai

import (
	"os"
	"testing"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfigLoadFromEnv(t *testing.T) {
	originalAPIKey := os.Getenv("REVENIUM_METERING_API_KEY")
	originalOpenAIKey := os.Getenv("OPENAI_API_KEY")
	defer func() {
		os.Setenv("REVENIUM_METERING_API_KEY", originalAPIKey)
		os.Setenv("OPENAI_API_KEY", originalOpenAIKey)
	}()

	os.Setenv("REVENIUM_METERING_API_KEY", "hak_test_key_123")
	os.Setenv("OPENAI_API_KEY", "sk-test-openai-key")

	cfg := &Config{}
	err := cfg.loadFromEnv()

	require.NoError(t, err)
	assert.Equal(t, "hak_test_key_123", cfg.Revenium.APIKey)
	assert.Equal(t, "sk-test-openai-key", cfg.OpenAIAPIKey)
}

func TestConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		config  *Config
		wantErr bool
	}{
		{
			name:    "valid config",
			config:  &Config{Revenium: &core.ReveniumConfig{APIKey: "hak_valid_key"}},
			wantErr: false,
		},
		{
			name:    "missing API key",
			config:  &Config{Revenium: &core.ReveniumConfig{}},
			wantErr: true,
		},
		{
			name:    "invalid API key format",
			config:  &Config{Revenium: &core.ReveniumConfig{APIKey: "invalid_key"}},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestWithOptions(t *testing.T) {
	cfg := &Config{}

	WithOpenAIAPIKey("test-key")(cfg)
	assert.Equal(t, "test-key", cfg.OpenAIAPIKey)

	WithReveniumAPIKey("hak_test")(cfg)
	assert.Equal(t, "hak_test", cfg.Revenium.APIKey)

	WithBaseURL("https://custom.api.com")(cfg)
	assert.Equal(t, "https://custom.api.com", cfg.BaseURL)

	WithAzureAPIKey("azure-key")(cfg)
	assert.Equal(t, "azure-key", cfg.AzureAPIKey)

	WithAzureEndpoint("https://azure.openai.com")(cfg)
	assert.Equal(t, "https://azure.openai.com", cfg.AzureEndpoint)

	WithAzureDisabled(true)(cfg)
	assert.True(t, cfg.AzureDisabled)

	WithDebug(true)(cfg)
	assert.True(t, cfg.Revenium.Debug)
}
