package google

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/revenium/revenium-go-sdk/core/enforcement"
	"github.com/revenium/revenium-go-sdk/core/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func resetSharedClient(t *testing.T) {
	t.Helper()
	enforcement.Stop()
	t.Cleanup(enforcement.Stop)
	t.Cleanup(func() { _ = sharedClient.ResetAndClose() })
}

func TestInitialize_SecondSetupForAnotherReveniumAccountIsRefused(t *testing.T) {
	resetSharedClient(t)
	metering := testutil.NewMockMeteringServer()
	t.Cleanup(metering.Close)
	rules := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(rules.Close)
	t.Setenv(core.EnvEnforcementBaseURL, rules.URL)
	t.Setenv(core.EnvTeamID, "")
	t.Setenv(core.EnvAPIKey, "")
	t.Setenv(core.EnvBaseURL, "")
	setUp := func(reveniumKey string) error {
		return Initialize(WithGoogleAPIKey("google-test-key"), WithReveniumAPIKey(reveniumKey), WithReveniumBaseURL(metering.URL()))
	}

	require.NoError(t, setUp("hak_customer_a"))
	err := setUp("hak_customer_b")

	require.Error(t, err)
	assert.True(t, core.IsConfigError(err))
	assert.Contains(t, err.Error(), core.EnvAPIKey)
	assert.Contains(t, err.Error(), "NewReveniumGoogle")
	assert.NoError(t, setUp("hak_customer_a"), "repeating the same setup is still allowed")
	assert.NoError(t, Initialize(WithGoogleAPIKey("google-test-key")), "a repeat call that names no Revenium account is not a different account")
	client, err := GetClient()
	require.NoError(t, err)
	assert.Equal(t, "hak_customer_a", client.config.Revenium.APIKey)
}
