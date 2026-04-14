package runway

import (
	"os"
	"testing"
	"time"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/stretchr/testify/assert"
)

func TestConfig_FunctionalOptions(t *testing.T) {
	cfg := &Config{}
	WithRunwayAPIKey("rk_test")(cfg)
	WithRunwayBaseURL("https://api.runwayml.example")(cfg)
	WithReveniumAPIKey("hak_test_key_123")(cfg)
	WithReveniumBaseURL("https://api.revenium.example")(cfg)
	WithRequestTimeout(45 * time.Second)(cfg)
	WithCapturePrompts(true)(cfg)

	assert.Equal(t, "rk_test", cfg.RunwayAPIKey)
	assert.Equal(t, "https://api.runwayml.example", cfg.RunwayBaseURL)
	assert.Equal(t, "hak_test_key_123", cfg.Revenium.APIKey)
	assert.Equal(t, "https://api.revenium.example", cfg.Revenium.BaseURL)
	assert.Equal(t, 45*time.Second, cfg.RequestTimeout)
	assert.True(t, cfg.CapturePrompts)
	assert.True(t, cfg.capturePromptsSet)
}

func TestConfig_Validate_RequiresRevenium(t *testing.T) {
	cfg := &Config{RunwayAPIKey: "rk_test"}
	err := cfg.Validate()
	assert.Error(t, err)
	assert.True(t, core.IsConfigError(err))
}

func TestConfig_Validate_RequiresRunwayKey(t *testing.T) {
	cfg := &Config{Revenium: &core.ReveniumConfig{APIKey: "hak_test_key_123"}}
	err := cfg.Validate()
	assert.Error(t, err)
	assert.True(t, core.IsConfigError(err))
	assert.Contains(t, err.Error(), "RUNWAY_API_KEY")
}

func TestConfig_Validate_OK(t *testing.T) {
	cfg := &Config{
		RunwayAPIKey: "rk_test",
		Revenium:     &core.ReveniumConfig{APIKey: "hak_test_key_123"},
	}
	assert.NoError(t, cfg.Validate())
}

func TestConfig_LoadFromEnv(t *testing.T) {
	os.Setenv("RUNWAY_API_KEY", "rk_env")
	os.Setenv("RUNWAY_BASE_URL", "https://runway.example.test")
	os.Setenv("RUNWAY_VERSION", "2025-01-01")
	os.Setenv("RUNWAY_REQUEST_TIMEOUT", "60s")
	os.Setenv("REVENIUM_METERING_API_KEY", "hak_env_key")
	defer func() {
		for _, k := range []string{"RUNWAY_API_KEY", "RUNWAY_BASE_URL", "RUNWAY_VERSION", "RUNWAY_REQUEST_TIMEOUT", "REVENIUM_METERING_API_KEY"} {
			os.Unsetenv(k)
		}
	}()

	cfg := &Config{}
	assert.NoError(t, cfg.LoadFromEnv())
	assert.Equal(t, "rk_env", cfg.RunwayAPIKey)
	assert.Equal(t, "https://runway.example.test", cfg.RunwayBaseURL)
	assert.Equal(t, "2025-01-01", cfg.RunwayVersion)
	assert.Equal(t, 60*time.Second, cfg.RequestTimeout)
}

func TestParseDurationFromEnv(t *testing.T) {
	t.Run("valid duration string", func(t *testing.T) {
		os.Setenv("X_DUR", "30s")
		defer os.Unsetenv("X_DUR")
		assert.Equal(t, 30*time.Second, parseDurationFromEnv("X_DUR", time.Minute))
	})
	t.Run("plain seconds", func(t *testing.T) {
		os.Setenv("X_DUR", "120")
		defer os.Unsetenv("X_DUR")
		assert.Equal(t, 120*time.Second, parseDurationFromEnv("X_DUR", time.Minute))
	})
	t.Run("missing returns default", func(t *testing.T) {
		os.Unsetenv("X_DUR")
		assert.Equal(t, time.Minute, parseDurationFromEnv("X_DUR", time.Minute))
	})
	t.Run("invalid returns default", func(t *testing.T) {
		os.Setenv("X_DUR", "garbage")
		defer os.Unsetenv("X_DUR")
		assert.Equal(t, time.Minute, parseDurationFromEnv("X_DUR", time.Minute))
	})
}
