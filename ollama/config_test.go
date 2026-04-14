package ollama

import (
	"os"
	"testing"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/stretchr/testify/assert"
)

func TestConfig_FunctionalOptions(t *testing.T) {
	cfg := &Config{}
	WithBaseURL("http://localhost:9999/v1")(cfg)
	WithReveniumAPIKey("hak_test_key_123")(cfg)
	WithReveniumBaseURL("https://api.revenium.example")(cfg)

	assert.Equal(t, "http://localhost:9999/v1", cfg.BaseURL)
	assert.Equal(t, "hak_test_key_123", cfg.Revenium.APIKey)
	assert.Equal(t, "https://api.revenium.example", cfg.Revenium.BaseURL)
}

func TestConfig_Validate_RequiresRevenium(t *testing.T) {
	cfg := &Config{}
	err := cfg.Validate()
	assert.Error(t, err)
	assert.True(t, core.IsConfigError(err))
}

func TestConfig_Validate_OK(t *testing.T) {
	cfg := &Config{Revenium: &core.ReveniumConfig{APIKey: "hak_test_key_123"}}
	assert.NoError(t, cfg.Validate())
}

func TestConfig_LoadFromEnv_DefaultsBaseURL(t *testing.T) {
	os.Unsetenv("OLLAMA_BASE_URL")
	os.Setenv("REVENIUM_METERING_API_KEY", "hak_env_key")
	defer os.Unsetenv("REVENIUM_METERING_API_KEY")

	cfg := &Config{}
	assert.NoError(t, cfg.loadFromEnv())
	assert.Equal(t, "http://localhost:11434/v1", cfg.BaseURL)
}

func TestConfig_LoadFromEnv_OverridesBaseURL(t *testing.T) {
	os.Setenv("OLLAMA_BASE_URL", "http://ollama.example.test")
	os.Setenv("REVENIUM_METERING_API_KEY", "hak_env_key")
	defer func() {
		os.Unsetenv("OLLAMA_BASE_URL")
		os.Unsetenv("REVENIUM_METERING_API_KEY")
	}()

	cfg := &Config{}
	assert.NoError(t, cfg.loadFromEnv())
	assert.Equal(t, "http://ollama.example.test", cfg.BaseURL)
}
