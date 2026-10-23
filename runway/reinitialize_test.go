package runway

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
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
		return Initialize(WithRunwayAPIKey("runway-test-key"), WithReveniumAPIKey(reveniumKey), WithReveniumBaseURL(metering.URL()))
	}

	require.NoError(t, setUp("hak_customer_a"))
	err := setUp("hak_customer_b")

	require.Error(t, err)
	assert.True(t, core.IsConfigError(err))
	assert.Contains(t, err.Error(), core.EnvAPIKey)
	assert.Contains(t, err.Error(), "NewReveniumRunway")
	assert.NoError(t, setUp("hak_customer_a"), "repeating the same setup is still allowed")
	assert.NoError(t, Initialize(WithRunwayAPIKey("runway-test-key")), "a repeat call that names no Revenium account is not a different account")
	client, err := GetClient()
	require.NoError(t, err)
	assert.Equal(t, "hak_customer_a", client.config.Revenium.APIKey)
}

func TestInitialize_ACallMadeWhileTheFirstSetupRunsGetsTheRepeatAnswer(t *testing.T) {
	cases := []struct {
		name       string
		secondOpts []Option
		wantErr    bool
	}{
		{name: "bare call", secondOpts: []Option{WithRunwayAPIKey("runway-test-key")}},
		{name: "call for another key", secondOpts: []Option{WithRunwayAPIKey("runway-test-key"), WithReveniumAPIKey("hak_customer_b")}, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resetSharedClient(t)
			metering := testutil.NewMockMeteringServer()
			t.Cleanup(metering.Close)
			fetchStarted := make(chan struct{})
			releaseFetch := make(chan struct{})
			var fetches atomic.Int32
			blockingRules := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if fetches.Add(1) == 1 {
					close(fetchStarted)
					<-releaseFetch
				}
				w.WriteHeader(http.StatusNoContent)
			}))
			t.Cleanup(blockingRules.Close)
			var releaseOnce sync.Once
			release := func() { releaseOnce.Do(func() { close(releaseFetch) }) }
			t.Cleanup(release)
			t.Setenv(core.EnvEnforcementBaseURL, blockingRules.URL)
			t.Setenv(core.EnvAPIKey, "")
			t.Setenv(core.EnvBaseURL, "")
			t.Setenv(core.EnvTeamID, "team-race")

			first := make(chan error, 1)
			go func() {
				first <- Initialize(WithRunwayAPIKey("runway-test-key"), WithReveniumAPIKey("hak_customer_a"), WithReveniumBaseURL(metering.URL()))
			}()
			<-fetchStarted
			second := make(chan error, 1)
			go func() { second <- Initialize(tc.secondOpts...) }()
			release()

			require.NoError(t, <-first)
			err := <-second
			client, getErr := GetClient()
			require.NoError(t, getErr)
			assert.Equal(t, "hak_customer_a", client.config.Revenium.APIKey, "the first setup stays in force")
			assert.Equal(t, int32(1), fetches.Load(), "the second call never built a client of its own")
			if !tc.wantErr {
				assert.NoError(t, err, "a call that names no other account gets nil whether or not the first setup had finished")
				return
			}
			require.Error(t, err)
			assert.True(t, core.IsConfigError(err))
			assert.Contains(t, err.Error(), core.EnvAPIKey)
			assert.Contains(t, err.Error(), "NewReveniumRunway")
		})
	}
}
