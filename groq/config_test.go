package groq

import (
	"os"
	"testing"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfig_FunctionalOptions(t *testing.T) {
	cfg := &Config{}
	WithGroqAPIKey("gsk_test")(cfg)
	WithReveniumAPIKey("hak_test_key_123")(cfg)
	WithReveniumBaseURL("https://api.revenium.example")(cfg)

	assert.Equal(t, "gsk_test", cfg.GroqAPIKey)
	assert.Equal(t, "hak_test_key_123", cfg.Revenium.APIKey)
	assert.Equal(t, "https://api.revenium.example", cfg.Revenium.BaseURL)
}

func TestConfig_Validate_RequiresRevenium(t *testing.T) {
	cfg := &Config{GroqAPIKey: "gsk_test"}
	err := cfg.Validate()
	assert.Error(t, err)
	assert.True(t, core.IsConfigError(err))
}

func TestConfig_Validate_RequiresValidAPIKey(t *testing.T) {
	cfg := &Config{
		GroqAPIKey: "gsk_test",
		Revenium:   &core.ReveniumConfig{APIKey: "invalid"},
	}
	err := cfg.Validate()
	assert.Error(t, err)
	assert.True(t, core.IsConfigError(err))
}

func TestConfig_Validate_OK(t *testing.T) {
	cfg := &Config{
		GroqAPIKey: "gsk_test",
		Revenium:   &core.ReveniumConfig{APIKey: "hak_test_key_123"},
	}
	assert.NoError(t, cfg.Validate())
}

func TestConfig_LoadFromEnv(t *testing.T) {
	os.Setenv("GROQ_API_KEY", "gsk_env")
	os.Setenv("GROQ_BASE_URL", "https://groq.example.test")
	os.Setenv("REVENIUM_METERING_API_KEY", "hak_env_key")
	defer func() {
		os.Unsetenv("GROQ_API_KEY")
		os.Unsetenv("GROQ_BASE_URL")
		os.Unsetenv("REVENIUM_METERING_API_KEY")
	}()

	cfg := &Config{}
	require.NoError(t, cfg.loadFromEnv())
	require.NotNil(t, cfg.Revenium)
	assert.Equal(t, "gsk_env", cfg.GroqAPIKey)
	assert.Equal(t, "https://groq.example.test", cfg.BaseURL)
	assert.Equal(t, "hak_env_key", cfg.Revenium.APIKey)
}

func TestConfig_LoadFromEnv_ProgrammaticOverridesEnv(t *testing.T) {
	os.Setenv("GROQ_API_KEY", "gsk_env")
	defer os.Unsetenv("GROQ_API_KEY")

	cfg := &Config{GroqAPIKey: "gsk_programmatic"}
	require.NoError(t, cfg.loadFromEnv())
	assert.Equal(t, "gsk_programmatic", cfg.GroqAPIKey)
}
