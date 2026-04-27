package litellm

import (
	"os"
	"testing"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/stretchr/testify/assert"
)

func TestValidateConfig_RequiresReveniumAPIKey(t *testing.T) {
	cfg := &Config{
		Revenium:        &core.ReveniumConfig{},
		LiteLLMProxyURL: "http://localhost:4000",
	}
	err := cfg.Validate()
	assert.Error(t, err)
	assert.True(t, core.IsConfigError(err))
	assert.Contains(t, err.Error(), "REVENIUM_METERING_API_KEY")
}

func TestValidateConfig_RequiresValidAPIKeyFormat(t *testing.T) {
	cfg := &Config{
		Revenium:        &core.ReveniumConfig{APIKey: "invalid_key"},
		LiteLLMProxyURL: "http://localhost:4000",
	}
	err := cfg.Validate()
	assert.Error(t, err)
	assert.True(t, core.IsConfigError(err))
	assert.Contains(t, err.Error(), "must start with 'hak_' or 'rev_'")
}

func TestValidateConfig_RequiresLiteLLMProxyURL(t *testing.T) {
	cfg := &Config{
		Revenium: &core.ReveniumConfig{APIKey: "hak_test_key_123"},
	}
	err := cfg.Validate()
	assert.Error(t, err)
	assert.True(t, core.IsConfigError(err))
	assert.Contains(t, err.Error(), "LITELLM_PROXY_URL")
}

func TestValidateConfig_Valid(t *testing.T) {
	cfg := &Config{
		Revenium:        &core.ReveniumConfig{APIKey: "hak_test_key_123"},
		LiteLLMProxyURL: "http://localhost:4000",
	}
	err := cfg.Validate()
	assert.NoError(t, err)
}

func TestNormalizeReveniumBaseURL(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"empty", "", core.DefaultReveniumBaseURL},
		{"with trailing slash", "https://api.revenium.ai/", "https://api.revenium.ai"},
		{"clean URL", "https://api.revenium.ai", "https://api.revenium.ai"},
		{"legacy /meter/v2", "https://api.revenium.ai/meter/v2", "https://api.revenium.ai"},
		{"legacy /meter", "https://api.revenium.ai/meter", "https://api.revenium.ai"},
		{"legacy /v2", "https://api.revenium.ai/v2", "https://api.revenium.ai"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := core.NormalizeReveniumBaseURL(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestFunctionalOptions(t *testing.T) {
	cfg := &Config{}
	WithLiteLLMProxyURL("http://localhost:4000")(cfg)
	WithLiteLLMAPIKey("sk-test")(cfg)
	WithReveniumAPIKey("hak_test_key")(cfg)
	WithReveniumBaseURL("https://custom.api.com")(cfg)
	WithDebug(true)(cfg)

	assert.Equal(t, "http://localhost:4000", cfg.LiteLLMProxyURL)
	assert.Equal(t, "sk-test", cfg.LiteLLMAPIKey)
	assert.Equal(t, "hak_test_key", cfg.Revenium.APIKey)
	assert.Equal(t, "https://custom.api.com", cfg.Revenium.BaseURL)
	assert.True(t, cfg.Revenium.Debug)
}

func TestLoadFromEnv(t *testing.T) {
	os.Setenv("REVENIUM_METERING_API_KEY", "hak_env_key")
	os.Setenv("LITELLM_PROXY_URL", "http://proxy:4000")
	os.Setenv("LITELLM_API_KEY", "sk-litellm")
	os.Setenv("REVENIUM_DEBUG", "true")
	defer func() {
		os.Unsetenv("REVENIUM_METERING_API_KEY")
		os.Unsetenv("LITELLM_PROXY_URL")
		os.Unsetenv("LITELLM_API_KEY")
		os.Unsetenv("REVENIUM_DEBUG")
	}()

	cfg := &Config{}
	err := cfg.loadFromEnv()
	assert.NoError(t, err)
	assert.Equal(t, "hak_env_key", cfg.Revenium.APIKey)
	assert.Equal(t, "http://proxy:4000", cfg.LiteLLMProxyURL)
	assert.Equal(t, "sk-litellm", cfg.LiteLLMAPIKey)
	assert.True(t, cfg.Revenium.Debug)
}

func TestLoadFromEnv_ProgrammaticOverridesEnv(t *testing.T) {
	os.Setenv("LITELLM_PROXY_URL", "http://env-proxy:4000")
	defer os.Unsetenv("LITELLM_PROXY_URL")

	cfg := &Config{
		LiteLLMProxyURL: "http://programmatic-proxy:4000",
	}
	err := cfg.loadFromEnv()
	assert.NoError(t, err)
	assert.Equal(t, "http://programmatic-proxy:4000", cfg.LiteLLMProxyURL)
}
