package metering

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func toolEventAddressServer(t *testing.T) (*httptest.Server, *[]string) {
	t.Helper()
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	return srv, &paths
}

func TestMeteringClient_ToolEventsGoToTheMeteringBaseURLSetting(t *testing.T) {
	srv, paths := toolEventAddressServer(t)
	t.Setenv(core.EnvBaseURL, srv.URL)
	client, err := NewMeteringClient(MeteringClientConfig{APIKey: "test-key"})
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })

	require.NoError(t, client.SendToolEventSync(NewToolEvent("weather-api").Build()))

	assert.Equal(t, []string{"/meter/v2/tool/events"}, *paths)
}

func TestMeteringClient_ExplicitBaseURLWinsOverTheSetting(t *testing.T) {
	explicit, explicitPaths := toolEventAddressServer(t)
	fromEnv, envPaths := toolEventAddressServer(t)
	t.Setenv(core.EnvBaseURL, fromEnv.URL)
	client, err := NewMeteringClient(MeteringClientConfig{APIKey: "test-key", BaseURL: explicit.URL})
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })

	require.NoError(t, client.SendToolEventSync(NewToolEvent("weather-api").Build()))

	assert.Equal(t, []string{"/meter/v2/tool/events"}, *explicitPaths)
	assert.Empty(t, *envPaths)
}

func TestNewMeteringClient_RejectsAnInvalidMeteringBaseURLSetting(t *testing.T) {
	t.Setenv(core.EnvBaseURL, "api.dev.example.test")

	_, err := NewMeteringClient(MeteringClientConfig{APIKey: "test-key"})

	require.Error(t, err)
	assert.True(t, core.IsConfigError(err))
	assert.Contains(t, err.Error(), core.EnvBaseURL)
}
