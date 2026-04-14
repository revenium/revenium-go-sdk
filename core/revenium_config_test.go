package core

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadReveniumConfig_Defaults(t *testing.T) {
	clearReveniumEnv(t)

	cfg := LoadReveniumConfig()

	assert.Equal(t, "", cfg.APIKey)
	assert.Equal(t, DefaultBaseURL, cfg.BaseURL)
	assert.Equal(t, "", cfg.OrgID)
	assert.Equal(t, "", cfg.ProductID)
	assert.Equal(t, DefaultLogLevel, cfg.LogLevel)
	assert.False(t, cfg.Debug)
	assert.False(t, cfg.VerboseStartup)
}

func TestLoadReveniumConfig_FromEnv(t *testing.T) {
	clearReveniumEnv(t)

	t.Setenv(EnvAPIKey, "hak_test123")
	t.Setenv(EnvBaseURL, "https://api.dev.hcapp.io")
	t.Setenv(EnvOrgID, "org-123")
	t.Setenv(EnvProductID, "prod-456")
	t.Setenv(EnvLogLevel, "DEBUG")
	t.Setenv(EnvDebug, "true")
	t.Setenv(EnvVerboseStartup, "1")

	cfg := LoadReveniumConfig()

	assert.Equal(t, "hak_test123", cfg.APIKey)
	assert.Equal(t, "https://api.dev.hcapp.io", cfg.BaseURL)
	assert.Equal(t, "org-123", cfg.OrgID)
	assert.Equal(t, "prod-456", cfg.ProductID)
	assert.Equal(t, "DEBUG", cfg.LogLevel)
	assert.True(t, cfg.Debug)
	assert.True(t, cfg.VerboseStartup)
}

func TestLoadReveniumConfig_NormalizesBaseURL(t *testing.T) {
	clearReveniumEnv(t)
	t.Setenv(EnvBaseURL, "https://api.revenium.ai/meter/v2")

	cfg := LoadReveniumConfig()

	assert.Equal(t, "https://api.revenium.ai", cfg.BaseURL)
}

func TestValidateReveniumConfig_Valid(t *testing.T) {
	cfg := &ReveniumConfig{APIKey: "hak_valid_key"}
	assert.NoError(t, ValidateReveniumConfig(cfg))
}

func TestValidateReveniumConfig_MissingKey(t *testing.T) {
	cfg := &ReveniumConfig{}
	err := ValidateReveniumConfig(cfg)
	require.Error(t, err)
	assert.True(t, IsConfigError(err))
}

func TestValidateReveniumConfig_InvalidFormat(t *testing.T) {
	cfg := &ReveniumConfig{APIKey: "sk_invalid"}
	err := ValidateReveniumConfig(cfg)
	require.Error(t, err)
	assert.True(t, IsConfigError(err))
}

func TestMergeReveniumConfig_NilProgrammatic(t *testing.T) {
	env := &ReveniumConfig{APIKey: "hak_env", BaseURL: "https://env.example.com"}
	result := MergeReveniumConfig(nil, env)
	assert.Equal(t, env, result)
}

func TestMergeReveniumConfig_ProgrammaticTakesPrecedence(t *testing.T) {
	programmatic := &ReveniumConfig{
		APIKey:  "hak_programmatic",
		BaseURL: "https://programmatic.example.com",
		OrgID:   "org-prog",
	}
	env := &ReveniumConfig{
		APIKey:    "hak_env",
		BaseURL:   "https://env.example.com",
		OrgID:     "org-env",
		ProductID: "prod-env",
		LogLevel:  "DEBUG",
		Debug:     true,
	}

	result := MergeReveniumConfig(programmatic, env)

	assert.Equal(t, "hak_programmatic", result.APIKey)
	assert.Equal(t, "https://programmatic.example.com", result.BaseURL)
	assert.Equal(t, "org-prog", result.OrgID)
	assert.Equal(t, "prod-env", result.ProductID)
	assert.Equal(t, "DEBUG", result.LogLevel)
	assert.True(t, result.Debug)
}

func TestMergeReveniumConfig_FillsEmptyFields(t *testing.T) {
	programmatic := &ReveniumConfig{APIKey: "hak_mine"}
	env := &ReveniumConfig{
		APIKey:         "hak_env",
		BaseURL:        DefaultBaseURL,
		LogLevel:       DefaultLogLevel,
		VerboseStartup: true,
	}

	result := MergeReveniumConfig(programmatic, env)

	assert.Equal(t, "hak_mine", result.APIKey)
	assert.Equal(t, DefaultBaseURL, result.BaseURL)
	assert.Equal(t, DefaultLogLevel, result.LogLevel)
	assert.True(t, result.VerboseStartup)
}

func TestValidateReveniumConfig_Nil(t *testing.T) {
	err := ValidateReveniumConfig(nil)
	require.Error(t, err)
	assert.True(t, IsConfigError(err))
}

func clearReveniumEnv(t *testing.T) {
	t.Helper()
	envVars := []string{EnvAPIKey, EnvBaseURL, EnvOrgID, EnvProductID, EnvLogLevel, EnvDebug, EnvVerboseStartup}
	for _, env := range envVars {
		t.Setenv(env, "")
		os.Unsetenv(env)
	}
}
