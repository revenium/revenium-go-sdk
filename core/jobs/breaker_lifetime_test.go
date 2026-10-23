package jobs

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/revenium/revenium-go-sdk/core/resilience"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func throttlingServer(t *testing.T) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.Header().Set("Retry-After", "60")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

func TestJobClient_KeepsAServerRequestedPauseAcrossCalls(t *testing.T) {
	resilience.ResetCircuitBreakers()
	t.Cleanup(resilience.ResetCircuitBreakers)
	srv, hits := throttlingServer(t)
	client := newTestClient(t, srv.URL)
	t.Cleanup(client.Close)

	_, err := client.GetJobTypes()
	require.Error(t, err)
	_, err = client.GetJobTypes()

	assert.ErrorIs(t, err, resilience.ErrCircuitOpen)
	assert.Equal(t, int32(1), hits.Load(), "the second call waits out the server's Retry-After")
}

func TestJobClient_CloseLetsALaterClientStartFresh(t *testing.T) {
	resilience.ResetCircuitBreakers()
	t.Cleanup(resilience.ResetCircuitBreakers)
	srv, hits := throttlingServer(t)
	first := newTestClient(t, srv.URL)
	_, _ = first.GetJobTypes()
	first.Close()
	first.Close()

	second := newTestClient(t, srv.URL)
	t.Cleanup(second.Close)
	_, err := second.GetJobTypes()

	assert.NotErrorIs(t, err, resilience.ErrCircuitOpen)
	assert.Equal(t, int32(2), hits.Load())
}

func TestJobClient_AnOutageReturnsTheServersErrorWithoutAWastedWait(t *testing.T) {
	resilience.ResetCircuitBreakers()
	t.Cleanup(resilience.ResetCircuitBreakers)
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error":"maintenance"}`))
	}))
	t.Cleanup(srv.Close)
	client := newTestClient(t, srv.URL)
	t.Cleanup(client.Close)
	client.retryConfig = &resilience.RetryConfig{MaxRetries: 3, BaseDelay: time.Millisecond, MaxDelay: 10 * time.Millisecond}

	_, err := client.GetJobTypes()

	require.Error(t, err)
	var revErr *core.ReveniumError
	require.True(t, errors.As(err, &revErr), "the caller still gets the server's answer, got %v", err)
	assert.Equal(t, http.StatusServiceUnavailable, revErr.StatusCode)
	assert.Contains(t, err.Error(), "maintenance")
	assert.ErrorIs(t, err, resilience.ErrCircuitOpen)
	assert.Equal(t, int32(3), hits.Load(), "the call stops once its own failures opened the breaker")
}

func TestJobClient_HasItsOwnBreakerSeparateFromUsageRecordsForTheSameKey(t *testing.T) {
	resilience.ResetCircuitBreakers()
	t.Cleanup(resilience.ResetCircuitBreakers)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`["CHAT"]`))
	}))
	t.Cleanup(srv.Close)
	metering, releaseMetering := resilience.AcquireCircuitBreaker(resilience.MeteringEndpoint, srv.URL, "hak_test_key")
	t.Cleanup(releaseMetering)
	metering.HoldOpenFor(time.Minute)
	client := newTestClient(t, srv.URL)
	t.Cleanup(client.Close)

	types, err := client.GetJobTypes()

	require.NoError(t, err, "a pause on the metering endpoint does not stop job calls")
	assert.Equal(t, []string{"CHAT"}, types)
}
