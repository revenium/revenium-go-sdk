package fal

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/revenium/revenium-go-sdk/core/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func imageServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"images":[{"url":"https://x/i.png","width":512,"height":512}]}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func clearOrganizationAndProductEnv(t *testing.T) {
	t.Helper()
	for _, name := range []string{core.EnvOrganizationName, core.EnvOrgID, core.EnvProductName, core.EnvProductID, core.EnvTeamID} {
		t.Setenv(name, "")
		require.NoError(t, os.Unsetenv(name))
	}
	t.Setenv(core.EnvEnvFilesDisable, "true")
}

func runAndCaptureRecord(t *testing.T, ctx context.Context, client *ReveniumFal, metering *testutil.MockMeteringServer, metadata map[string]interface{}) map[string]interface{} {
	t.Helper()
	_, err := client.Run(ctx, "fal-ai/flux/schnell", map[string]interface{}{"prompt": "cat"}, metadata)
	require.NoError(t, err)
	client.Flush()
	require.True(t, metering.WaitForPayloads(1, 2*time.Second))
	return metering.GetPayloads()[0]
}

func TestInitialize_OrganizationAndProductOptionsReachTheRecord(t *testing.T) {
	resetSharedClient(t)
	clearOrganizationAndProductEnv(t)
	t.Setenv(core.EnvOrganizationName, "from-env")
	fal := imageServer(t)
	metering := testutil.NewMockMeteringServer()
	t.Cleanup(metering.Close)

	require.NoError(t, Initialize(
		WithFalAPIKey("test-key"),
		WithReveniumAPIKey("hak_test_key_123"),
		WithReveniumBaseURL(metering.URL()),
		WithReveniumOrgID("acme"),
		WithReveniumProductID("storefront"),
		func(c *Config) { c.FalBaseURL, c.QueueBaseURL = fal.URL, fal.URL },
	))
	client, err := GetClient()
	require.NoError(t, err)

	record := runAndCaptureRecord(t, context.Background(), client, metering, nil)

	assert.Equal(t, "acme", record["organizationName"])
	assert.Equal(t, "storefront", record["productName"])
}

func TestInitialize_OrganizationAndProductFromTheEnvironmentReachTheRecord(t *testing.T) {
	resetSharedClient(t)
	clearOrganizationAndProductEnv(t)
	t.Setenv(core.EnvOrganizationName, "acme")
	t.Setenv(core.EnvProductName, "storefront")
	fal := imageServer(t)
	metering := testutil.NewMockMeteringServer()
	t.Cleanup(metering.Close)

	require.NoError(t, Initialize(
		WithFalAPIKey("test-key"),
		WithReveniumAPIKey("hak_test_key_123"),
		WithReveniumBaseURL(metering.URL()),
		func(c *Config) { c.FalBaseURL, c.QueueBaseURL = fal.URL, fal.URL },
	))
	client, err := GetClient()
	require.NoError(t, err)

	record := runAndCaptureRecord(t, context.Background(), client, metering, map[string]interface{}{"productName": "per-call"})

	assert.Equal(t, "acme", record["organizationName"])
	assert.Equal(t, "per-call", record["productName"], "a value set for the call wins")
}
