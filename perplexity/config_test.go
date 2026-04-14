package perplexity

import (
	"os"
	"testing"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfigLoadFromEnv(t *testing.T) {
	keys := []string{
		"REVENIUM_METERING_API_KEY",
		"REVENIUM_API_KEY",
		"REVENIUM_METERING_BASE_URL",
		"REVENIUM_BASE_URL",
		"PERPLEXITY_API_KEY",
		"PERPLEXITY_API_BASE_URL",
		"REVENIUM_DEBUG",
	}

	original := make(map[string]string)
	for _, k := range keys {
		original[k] = os.Getenv(k)
	}
	t.Cleanup(func() {
		for k, v := range original {
			_ = os.Setenv(k, v)
		}
	})

	t.Run("uses explicit env vars", func(t *testing.T) {
		_ = os.Setenv("REVENIUM_METERING_API_KEY", "hak_test_key_123")
		_ = os.Setenv("REVENIUM_API_KEY", "")
		_ = os.Setenv("PERPLEXITY_API_KEY", "pplx-test-key")
		_ = os.Setenv("REVENIUM_METERING_BASE_URL", "https://metering.example.com")
		_ = os.Setenv("REVENIUM_BASE_URL", "")
		_ = os.Setenv("PERPLEXITY_API_BASE_URL", "https://perplexity.example.com")
		_ = os.Setenv("REVENIUM_DEBUG", "true")

		cfg := &Config{}
		err := cfg.loadFromEnv()

		require.NoError(t, err)
		assert.Equal(t, "hak_test_key_123", cfg.Revenium.APIKey)
		assert.Equal(t, "https://metering.example.com", cfg.Revenium.BaseURL)
		assert.Equal(t, "pplx-test-key", cfg.PerplexityAPIKey)
		assert.Equal(t, "https://perplexity.example.com", cfg.PerplexityBaseURL)
		assert.True(t, cfg.Revenium.Debug)
	})

	t.Run("uses fallback keys and defaults", func(t *testing.T) {
		_ = os.Setenv("REVENIUM_METERING_API_KEY", "")
		_ = os.Setenv("REVENIUM_API_KEY", "hak_fallback_key")
		_ = os.Setenv("PERPLEXITY_API_KEY", "pplx-fallback-key")
		_ = os.Setenv("REVENIUM_METERING_BASE_URL", "")
		_ = os.Setenv("REVENIUM_BASE_URL", "")
		_ = os.Setenv("PERPLEXITY_API_BASE_URL", "")
		_ = os.Setenv("REVENIUM_DEBUG", "false")

		cfg := &Config{}
		err := cfg.loadFromEnv()

		require.NoError(t, err)
		assert.Equal(t, "hak_fallback_key", cfg.Revenium.APIKey)
		assert.Equal(t, core.DefaultBaseURL, cfg.Revenium.BaseURL)
		assert.Equal(t, "pplx-fallback-key", cfg.PerplexityAPIKey)
		assert.Equal(t, defaultPerplexityBaseURL, cfg.PerplexityBaseURL)
		assert.False(t, cfg.Revenium.Debug)
	})
}

func TestConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		config  *Config
		wantErr bool
	}{
		{
			name: "valid config",
			config: &Config{
				Revenium:         &core.ReveniumConfig{APIKey: "hak_valid_key"},
				PerplexityAPIKey: "pplx-valid-key",
			},
			wantErr: false,
		},
		{
			name: "missing Revenium API key",
			config: &Config{
				Revenium:         &core.ReveniumConfig{},
				PerplexityAPIKey: "pplx-valid-key",
			},
			wantErr: true,
		},
		{
			name: "invalid Revenium API key prefix",
			config: &Config{
				Revenium:         &core.ReveniumConfig{APIKey: "invalid_key"},
				PerplexityAPIKey: "pplx-valid-key",
			},
			wantErr: true,
		},
		{
			name: "missing Perplexity API key",
			config: &Config{
				Revenium:         &core.ReveniumConfig{APIKey: "hak_valid_key"},
				PerplexityAPIKey: "",
			},
			wantErr: true,
		},
		{
			name: "invalid Perplexity API key prefix",
			config: &Config{
				Revenium:         &core.ReveniumConfig{APIKey: "hak_valid_key"},
				PerplexityAPIKey: "sk-invalid-prefix",
			},
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

	WithPerplexityAPIKey("pplx-key")(cfg)
	assert.Equal(t, "pplx-key", cfg.PerplexityAPIKey)

	WithPerplexityBaseURL("https://perplexity.example.com")(cfg)
	assert.Equal(t, "https://perplexity.example.com", cfg.PerplexityBaseURL)

	WithReveniumAPIKey("hak-key")(cfg)
	assert.Equal(t, "hak-key", cfg.Revenium.APIKey)

	WithReveniumBaseURL("https://metering.example.com")(cfg)
	assert.Equal(t, "https://metering.example.com", cfg.Revenium.BaseURL)

	WithDebug(true)(cfg)
	assert.True(t, cfg.Revenium.Debug)
}
