package core

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetEnvOrDefault(t *testing.T) {
	originalVal := os.Getenv("TEST_REVENIUM_ENV_VAR")
	defer os.Setenv("TEST_REVENIUM_ENV_VAR", originalVal)

	// When env var is set
	os.Setenv("TEST_REVENIUM_ENV_VAR", "custom_value")
	assert.Equal(t, "custom_value", GetEnvOrDefault("TEST_REVENIUM_ENV_VAR", "default"))

	// When env var is not set
	os.Unsetenv("TEST_REVENIUM_ENV_VAR")
	assert.Equal(t, "default", GetEnvOrDefault("TEST_REVENIUM_ENV_VAR", "default"))

	// When env var is empty
	os.Setenv("TEST_REVENIUM_ENV_VAR", "")
	assert.Equal(t, "default", GetEnvOrDefault("TEST_REVENIUM_ENV_VAR", "default"))
}

func TestIsValidAPIKeyFormat(t *testing.T) {
	tests := []struct {
		name     string
		key      string
		expected bool
	}{
		{
			name:     "valid key",
			key:      "hak_1234567890",
			expected: true,
		},
		{
			name:     "valid rev_ key",
			key:      "rev_1234567890",
			expected: true,
		},
		{
			name:     "invalid prefix",
			key:      "sk_1234567890",
			expected: false,
		},
		{
			name:     "too short",
			key:      "hak",
			expected: false,
		},
		{
			name:     "empty",
			key:      "",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsValidAPIKeyFormat(tt.key)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestNormalizeReveniumBaseURL(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "empty string",
			input:    "",
			expected: "https://api.revenium.ai",
		},
		{
			name:     "with trailing slash",
			input:    "https://api.revenium.ai/",
			expected: "https://api.revenium.ai",
		},
		{
			name:     "with /meter/v2",
			input:    "https://api.revenium.ai/meter/v2",
			expected: "https://api.revenium.ai",
		},
		{
			name:     "with /meter",
			input:    "https://api.revenium.ai/meter",
			expected: "https://api.revenium.ai",
		},
		{
			name:     "with /v2",
			input:    "https://api.revenium.ai/v2",
			expected: "https://api.revenium.ai",
		},
		{
			name:     "clean URL",
			input:    "https://api.revenium.ai",
			expected: "https://api.revenium.ai",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := NormalizeReveniumBaseURL(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestDefaultReveniumBaseURL(t *testing.T) {
	assert.Equal(t, "https://api.revenium.ai", DefaultReveniumBaseURL)
}
