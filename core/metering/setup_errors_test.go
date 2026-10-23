package metering

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/revenium/revenium-go-sdk/core/resilience"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func countingServer(t *testing.T) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

func unvalidatedClient(t *testing.T, cfg MeteringClientConfig) *MeteringClient {
	t.Helper()
	breaker, release := resilience.AcquireCircuitBreaker(resilience.MeteringEndpoint, cfg.BaseURL, cfg.APIKey)
	client := newMeteringClient(cfg, breaker, release)
	client.retryConfig = testRetryConfig
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func TestNewMeteringClient_RejectsBaseURLWithoutScheme(t *testing.T) {
	_, err := NewMeteringClient(MeteringClientConfig{APIKey: "hak_test_key", BaseURL: "api.revenium.ai"})

	require.Error(t, err)
	assert.True(t, core.IsConfigError(err))
	assert.Contains(t, err.Error(), "configured Revenium BaseURL")
	assert.NotContains(t, err.Error(), core.EnvBaseURL, "the bad value came from the config, not the environment")
}

func TestNewMeteringClient_RejectsKeyWithLineBreakInside(t *testing.T) {
	_, err := NewMeteringClient(MeteringClientConfig{APIKey: "hak_test\nkey", BaseURL: "https://api.revenium.ai"})

	require.Error(t, err)
	assert.True(t, core.IsConfigError(err))
	assert.Contains(t, err.Error(), core.EnvAPIKey)
}

func TestNewMeteringClient_TrimsPastedWhitespaceLikeInitialize(t *testing.T) {
	var receivedKey string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedKey = r.Header.Get("x-api-key")
		w.WriteHeader(http.StatusCreated)
	}))
	t.Cleanup(srv.Close)

	client, err := NewMeteringClient(MeteringClientConfig{APIKey: "hak_test_key\n", BaseURL: " " + srv.URL + "\n"})
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })

	require.NoError(t, client.SendSync(NewPayload(OperationChat, "gpt-4", "OPENAI").Build()))
	assert.Equal(t, "hak_test_key", receivedKey)
}

func TestSendSync_RequestThatCannotBeBuiltIsNotRetriedBufferedOrCountedAgainstRevenium(t *testing.T) {
	resilience.ResetCircuitBreakers()
	t.Cleanup(resilience.ResetCircuitBreakers)
	srv, hits := countingServer(t)
	cases := map[string]MeteringClientConfig{
		"base URL without scheme": {APIKey: "hak_test_key", BaseURL: "api.revenium.ai"},
		"key with line break":     {APIKey: "hak_test_key\n", BaseURL: srv.URL},
	}

	for name, cfg := range cases {
		t.Run(name, func(t *testing.T) {
			client := unvalidatedClient(t, cfg)

			for i := 0; i < 10; i++ {
				err := client.SendSync(NewPayload(OperationChat, "gpt-4", "OPENAI").Build())
				require.Error(t, err)
				assert.True(t, core.IsConfigError(err), "got %v", err)
			}

			assert.Equal(t, int32(0), hits.Load())
			assert.Equal(t, 0, client.GetBufferStats().Size)
			assert.Equal(t, resilience.StateClosed, client.CircuitBreakerState())
		})
	}
}

func TestSendSync_HealthyClientDeliversOnFirstTryAfterMisconfiguredClientFails(t *testing.T) {
	resilience.ResetCircuitBreakers()
	t.Cleanup(resilience.ResetCircuitBreakers)
	srv, hits := countingServer(t)
	broken := unvalidatedClient(t, MeteringClientConfig{APIKey: "hak_test_key\n", BaseURL: srv.URL})
	healthy := newTestClient(t, srv.URL)

	for i := 0; i < 6; i++ {
		_ = broken.SendSync(NewPayload(OperationChat, "gpt-4", "OPENAI").Build())
	}
	assert.Equal(t, resilience.StateClosed, broken.CircuitBreakerState())

	require.NoError(t, healthy.SendSync(NewPayload(OperationChat, "gpt-4", "OPENAI").Build()))
	assert.Equal(t, int32(1), hits.Load())
}

func TestFlush_DiscardsBufferedRecordThatCannotBeSentAndDeliversTheRest(t *testing.T) {
	srv, hits := countingServer(t)
	buf := newTestBuffer(10)
	t.Cleanup(buf.Stop)
	buf.Push(BufferedEvent{URL: srv.URL, Headers: map[string]string{"x-api-key": "hak_test_key\n"}, Body: []byte(`{}`)})
	buf.Push(BufferedEvent{URL: srv.URL, Headers: map[string]string{"x-api-key": "hak_test_key"}, Body: []byte(`{}`)})

	buf.flush()

	stats := buf.GetBufferStats()
	assert.Equal(t, 0, stats.Size)
	assert.Equal(t, int64(1), stats.EventsReplayed)
	assert.Equal(t, int64(1), stats.EventsEvicted)
	assert.Equal(t, int32(1), hits.Load())
}
