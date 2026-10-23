package openai

import (
	"context"
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
		return Initialize(WithOpenAIAPIKey("sk-test"), WithAzureDisabled(true), WithReveniumAPIKey(reveniumKey), WithReveniumBaseURL(metering.URL()))
	}

	require.NoError(t, setUp("hak_customer_a"))
	err := setUp("hak_customer_b")

	require.Error(t, err)
	assert.True(t, core.IsConfigError(err))
	assert.Contains(t, err.Error(), core.EnvAPIKey)
	assert.Contains(t, err.Error(), "NewReveniumOpenAI")
	assert.NoError(t, setUp("hak_customer_a"), "repeating the same setup is still allowed")
	assert.NoError(t, Initialize(WithOpenAIAPIKey("sk-test"), WithAzureDisabled(true)), "a repeat call that names no Revenium account is not a different account")
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
		{name: "bare call", secondOpts: []Option{WithOpenAIAPIKey("sk-test"), WithAzureDisabled(true)}},
		{name: "call for another key", secondOpts: []Option{WithOpenAIAPIKey("sk-test"), WithAzureDisabled(true), WithReveniumAPIKey("hak_customer_b")}, wantErr: true},
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
				first <- Initialize(WithOpenAIAPIKey("sk-test"), WithAzureDisabled(true), WithReveniumAPIKey("hak_customer_a"), WithReveniumBaseURL(metering.URL()))
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
			assert.Contains(t, err.Error(), "NewReveniumOpenAI")
		})
	}
}

func TestStandaloneClients_EachAccountUsesItsOwnReveniumKey(t *testing.T) {
	enforcement.Stop()
	t.Cleanup(enforcement.Stop)
	provider := newProviderStub(t)
	metering := testutil.NewMockMeteringServer()
	t.Cleanup(metering.Close)
	var mu sync.Mutex
	var ruleFetchKeys []string
	rules := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		ruleFetchKeys = append(ruleFetchKeys, r.Header.Get("x-api-key"))
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(rules.Close)
	newAccountClient := func(reveniumKey, teamID string) *ReveniumOpenAI {
		client, err := NewReveniumOpenAI(&Config{
			OpenAIAPIKey: "sk-test",
			BaseURL:      provider.server.URL,
			Revenium: &core.ReveniumConfig{
				APIKey:             reveniumKey,
				BaseURL:            metering.URL(),
				EnforcementBaseURL: rules.URL,
				TeamID:             teamID,
			},
		})
		require.NoError(t, err)
		return client
	}
	customerA := newAccountClient("hak_customer_a", "team-a")
	customerB := newAccountClient("hak_customer_b", "team-b")

	_, err := customerA.Chat().Completions().New(context.Background(), chatParams("gpt-4.1-nano"))
	require.NoError(t, err)
	_, err = customerB.Chat().Completions().New(context.Background(), chatParams("gpt-4.1-nano"))
	require.NoError(t, err)
	require.NoError(t, customerA.Close())
	require.NoError(t, customerB.Close())

	var recordKeys []string
	for _, request := range metering.GetRequests() {
		recordKeys = append(recordKeys, request.Headers.Get("x-api-key"))
	}
	assert.ElementsMatch(t, []string{"hak_customer_a", "hak_customer_b"}, recordKeys)
	mu.Lock()
	defer mu.Unlock()
	assert.Contains(t, ruleFetchKeys, "hak_customer_a")
	assert.Contains(t, ruleFetchKeys, "hak_customer_b")
}

func TestStandaloneClient_AcceptsAKeyPastedWithATrailingNewline(t *testing.T) {
	enforcement.Stop()
	t.Cleanup(enforcement.Stop)
	provider := newProviderStub(t)
	metering := testutil.NewMockMeteringServer()
	t.Cleanup(metering.Close)
	rules := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(rules.Close)

	client, err := NewReveniumOpenAI(&Config{
		OpenAIAPIKey: "sk-test",
		BaseURL:      provider.server.URL,
		Revenium: &core.ReveniumConfig{
			APIKey:             "hak_customer_a\n",
			BaseURL:            metering.URL() + "\n",
			EnforcementBaseURL: rules.URL,
		},
	})
	require.NoError(t, err)
	_, err = client.Chat().Completions().New(context.Background(), chatParams("gpt-4.1-nano"))
	require.NoError(t, err)
	require.NoError(t, client.Close())

	requests := metering.GetRequests()
	require.Len(t, requests, 1)
	assert.Equal(t, "hak_customer_a", requests[0].Headers.Get("x-api-key"))
}
