package core

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveReveniumBaseURL_UsesTheConfiguredAddressFirst(t *testing.T) {
	t.Setenv(EnvBaseURL, "https://api.dev.example.test")

	assert.Equal(t, "https://api.eu.example.test", ResolveReveniumBaseURL(" https://api.eu.example.test/meter/v2 "))
}

func TestResolveReveniumBaseURL_FallsBackToTheMeteringBaseURLSetting(t *testing.T) {
	t.Setenv(EnvBaseURL, " https://api.dev.example.test/ ")

	assert.Equal(t, "https://api.dev.example.test", ResolveReveniumBaseURL(""))
}

func TestResolveReveniumBaseURL_DefaultsWhenNothingIsSet(t *testing.T) {
	t.Setenv(EnvBaseURL, "")

	assert.Equal(t, DefaultReveniumBaseURL, ResolveReveniumBaseURL("  "))
}

func TestResolveValidReveniumBaseURL_NamesTheConfiguredSettingForABadConfiguredValue(t *testing.T) {
	t.Setenv(EnvBaseURL, "https://api.dev.example.test")

	_, err := ResolveValidReveniumBaseURL("api.eu.example.test", "Config.BaseURL")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "Config.BaseURL")
	assert.NotContains(t, err.Error(), EnvBaseURL)
}

func TestResolveValidReveniumBaseURL_NamesTheEnvironmentForABadEnvironmentValue(t *testing.T) {
	t.Setenv(EnvBaseURL, "api.dev.example.test")

	_, err := ResolveValidReveniumBaseURL("", "Config.BaseURL")

	require.Error(t, err)
	assert.Contains(t, err.Error(), EnvBaseURL)
	assert.NotContains(t, err.Error(), "Config.BaseURL")
}

func TestResolveValidReveniumBaseURL_ReturnsTheResolvedAddress(t *testing.T) {
	t.Setenv(EnvBaseURL, "https://api.dev.example.test/")

	baseURL, err := ResolveValidReveniumBaseURL("", "Config.BaseURL")

	require.NoError(t, err)
	assert.Equal(t, "https://api.dev.example.test", baseURL)
}
