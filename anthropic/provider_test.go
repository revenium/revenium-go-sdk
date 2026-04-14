package anthropic

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDetectProvider_NilConfig(t *testing.T) {
	assert.Equal(t, ProviderAnthropic, DetectProvider(nil))
}

func TestDetectProvider_DefaultAnthropic(t *testing.T) {
	cfg := &Config{}
	assert.Equal(t, ProviderAnthropic, DetectProvider(cfg))
}

func TestDetectProvider_BedrockDisabled(t *testing.T) {
	cfg := &Config{
		AWSAccessKeyID:     "key",
		AWSSecretAccessKey: "secret",
		BedrockDisabled:    true,
	}
	assert.Equal(t, ProviderAnthropic, DetectProvider(cfg))
}

func TestDetectProvider_AWSCredentials(t *testing.T) {
	cfg := &Config{
		AWSAccessKeyID:     "key",
		AWSSecretAccessKey: "secret",
	}
	assert.Equal(t, ProviderBedrock, DetectProvider(cfg))
}

func TestDetectProvider_AWSBaseURL(t *testing.T) {
	cfg := &Config{
		BaseURL: "https://bedrock-runtime.us-east-1.amazonaws.com",
	}
	assert.Equal(t, ProviderBedrock, DetectProvider(cfg))
}

func TestProvider_IsAnthropic(t *testing.T) {
	assert.True(t, ProviderAnthropic.IsAnthropic())
	assert.False(t, ProviderBedrock.IsAnthropic())
}

func TestProvider_IsBedrock(t *testing.T) {
	assert.True(t, ProviderBedrock.IsBedrock())
	assert.False(t, ProviderAnthropic.IsBedrock())
}

func TestProvider_String(t *testing.T) {
	assert.Equal(t, "ANTHROPIC", ProviderAnthropic.String())
	assert.Equal(t, "AWS", ProviderBedrock.String())
}
