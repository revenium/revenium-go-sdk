package openai

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/revenium/revenium-go-sdk/core/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInitialize_SecondSetupWithAnotherCostControlKeyOrAddressIsRefused(t *testing.T) {
	cases := []struct {
		name    string
		setting string
		value   string
	}{
		{name: "rules key", setting: core.EnvEnforcementAPIKey, value: "rev_rk_customer_b"},
		{name: "rules address", setting: core.EnvEnforcementBaseURL, value: "https://rules-b.example"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resetSharedClient(t)
			metering := testutil.NewMockMeteringServer()
			t.Cleanup(metering.Close)
			rules := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNoContent)
			}))
			t.Cleanup(rules.Close)
			t.Setenv(core.EnvTeamID, "")
			t.Setenv(core.EnvAPIKey, "")
			t.Setenv(core.EnvBaseURL, "")
			t.Setenv(core.EnvEnforcementBaseURL, rules.URL)
			t.Setenv(core.EnvEnforcementAPIKey, "rev_rk_customer_a")
			setUp := func() error {
				return Initialize(WithOpenAIAPIKey("sk-test"), WithAzureDisabled(true), WithReveniumAPIKey("hak_customer_a"), WithReveniumBaseURL(metering.URL()))
			}
			require.NoError(t, setUp())
			require.NoError(t, setUp(), "repeating the same setup is still allowed")

			t.Setenv(tc.setting, tc.value)
			err := setUp()

			require.Error(t, err)
			assert.True(t, core.IsConfigError(err))
			assert.Contains(t, err.Error(), tc.setting)
			assert.NotContains(t, err.Error(), "rev_rk_customer")
		})
	}
}
