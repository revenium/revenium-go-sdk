package grok

import (
	"os"
	"testing"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfig_FunctionalOptions(t *testing.T) {
	cfg := &Config{}
	WithXAIAPIKey("xai_test")(cfg)
	WithReveniumAPIKey("hak_test_key_123")(cfg)
	WithReveniumBaseURL("https://api.revenium.example")(cfg)

	assert.Equal(t, "xai_test", cfg.XAIAPIKey)
	assert.Equal(t, "hak_test_key_123", cfg.Revenium.APIKey)
	assert.Equal(t, "https://api.revenium.example", cfg.Revenium.BaseURL)
}

func TestConfig_Validate_RequiresRevenium(t *testing.T) {
	cfg := &Config{XAIAPIKey: "x"}
	err := cfg.Validate()
	assert.Error(t, err)
	assert.True(t, core.IsConfigError(err))
}

func TestConfig_Validate_OK(t *testing.T) {
	cfg := &Config{
		XAIAPIKey: "x",
		Revenium:  &core.ReveniumConfig{APIKey: "hak_test_key_123"},
	}
	assert.NoError(t, cfg.Validate())
}

func TestConfig_LoadFromEnv(t *testing.T) {
	os.Setenv("XAI_API_KEY", "xai_env")
	os.Setenv("XAI_BASE_URL", "https://x.example.test")
	os.Setenv("REVENIUM_METERING_API_KEY", "hak_env_key")
	defer func() {
		os.Unsetenv("XAI_API_KEY")
		os.Unsetenv("XAI_BASE_URL")
		os.Unsetenv("REVENIUM_METERING_API_KEY")
	}()

	cfg := &Config{}
	require.NoError(t, cfg.loadFromEnv())
	require.NotNil(t, cfg.Revenium)
	assert.Equal(t, "xai_env", cfg.XAIAPIKey)
	assert.Equal(t, "https://x.example.test", cfg.BaseURL)
	assert.Equal(t, "hak_env_key", cfg.Revenium.APIKey)
}
