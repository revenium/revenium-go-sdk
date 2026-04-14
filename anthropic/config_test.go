package anthropic

import (
	"os"
	"testing"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfig_Validate_MissingRevenium(t *testing.T) {
	cfg := &Config{}
	err := cfg.Validate()
	assert.Error(t, err)
}

func TestConfig_Validate_MissingAPIKey(t *testing.T) {
	cfg := &Config{
		Revenium: &core.ReveniumConfig{},
	}
	err := cfg.Validate()
	assert.Error(t, err)
}

func TestConfig_Validate_ValidConfig(t *testing.T) {
	cfg := &Config{
		Revenium: &core.ReveniumConfig{
			APIKey:  "hak_test123",
			BaseURL: "https://api.test.io",
		},
	}
	err := cfg.Validate()
	assert.NoError(t, err)
}

func TestConfig_LoadFromEnv(t *testing.T) {
	os.Setenv("ANTHROPIC_API_KEY", "sk-ant-test")
	os.Setenv("AWS_REGION", "eu-west-1")
	os.Setenv("REVENIUM_BEDROCK_DISABLE", "true")
	defer func() {
		os.Unsetenv("ANTHROPIC_API_KEY")
		os.Unsetenv("AWS_REGION")
		os.Unsetenv("REVENIUM_BEDROCK_DISABLE")
	}()

	cfg := &Config{}
	err := cfg.loadFromEnv()
	require.NoError(t, err)

	assert.Equal(t, "sk-ant-test", cfg.AnthropicAPIKey)
	assert.Equal(t, "eu-west-1", cfg.AWSRegion)
	assert.True(t, cfg.BedrockDisabled)
}

func TestConfig_Options(t *testing.T) {
	cfg := &Config{}

	WithAnthropicAPIKey("my-key")(cfg)
	assert.Equal(t, "my-key", cfg.AnthropicAPIKey)

	WithReveniumAPIKey("hak_abc")(cfg)
	assert.Equal(t, "hak_abc", cfg.Revenium.APIKey)

	WithReveniumBaseURL("https://api.test.io")(cfg)
	assert.Equal(t, "https://api.test.io", cfg.Revenium.BaseURL)

	WithAWSRegion("us-west-2")(cfg)
	assert.Equal(t, "us-west-2", cfg.AWSRegion)

	WithBedrockDisabled(true)(cfg)
	assert.True(t, cfg.BedrockDisabled)
}

func TestConfig_LoadFromEnv_ProgrammaticTakesPrecedence(t *testing.T) {
	os.Setenv("ANTHROPIC_API_KEY", "env-key")
	defer os.Unsetenv("ANTHROPIC_API_KEY")

	cfg := &Config{AnthropicAPIKey: "programmatic-key"}
	err := cfg.loadFromEnv()
	require.NoError(t, err)

	assert.Equal(t, "programmatic-key", cfg.AnthropicAPIKey)
}
