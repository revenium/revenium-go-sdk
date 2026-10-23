package metering

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/revenium/revenium-go-sdk/core/resilience"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testClientTimeout = 50 * time.Millisecond

type slowServer struct {
	*httptest.Server
	slow atomic.Bool
	mu   sync.Mutex
	keys []string
}

func newSlowServer(t *testing.T) *slowServer {
	t.Helper()
	s := &slowServer{}
	s.slow.Store(true)
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.keys = append(s.keys, r.Header.Get("Idempotency-Key"))
		s.mu.Unlock()
		if s.slow.Load() {
			time.Sleep(4 * testClientTimeout)
		}
		w.WriteHeader(http.StatusCreated)
	}))
	t.Cleanup(s.Close)
	return s
}

func (s *slowServer) Keys() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.keys...)
}

func newTimeoutTestClient(t *testing.T, baseURL string) *MeteringClient {
	t.Helper()
	client := newTestClient(t, baseURL)
	client.path.sender = &recordSender{httpClient: sharedHTTPClient, timeout: testClientTimeout}
	return client
}

func TestSendSync_RecordThatTimesOutIsRetriedBufferedAndDeliveredOnceReveniumRecovers(t *testing.T) {
	resilience.ResetCircuitBreakers()
	t.Cleanup(resilience.ResetCircuitBreakers)
	srv := newSlowServer(t)
	client := newTimeoutTestClient(t, srv.URL)
	payload := NewPayload(OperationChat, "gpt-4", "OPENAI").Build()

	err := client.SendSync(payload)

	require.Error(t, err)
	assert.Equal(t, resilience.ClassificationRetryable, resilience.ClassifyError(err), "the SDK's own request deadline is retryable")
	assert.Greater(t, len(srv.Keys()), 1, "a timed-out send is retried")
	assert.Equal(t, 1, client.GetBufferStats().Size, "a send that keeps timing out is held for replay")

	srv.slow.Store(false)
	client.Flush()

	assert.Equal(t, 0, client.GetBufferStats().Size)
	assert.Equal(t, int64(1), client.GetBufferStats().EventsReplayed)
	for _, key := range srv.Keys() {
		assert.Equal(t, payload.IdempotencyKey, key, "every attempt carries the record's one idempotency key")
	}
}

func TestSendSync_RepeatedTimeoutsOpenTheCircuitBreaker(t *testing.T) {
	resilience.ResetCircuitBreakers()
	t.Cleanup(resilience.ResetCircuitBreakers)
	srv := newSlowServer(t)
	client := newTimeoutTestClient(t, srv.URL)
	client.retryConfig = &resilience.RetryConfig{MaxRetries: 0}

	for i := 0; i < 5; i++ {
		_ = client.SendSync(NewPayload(OperationChat, "gpt-4", "OPENAI").Build())
	}

	assert.Equal(t, resilience.StateOpen, client.CircuitBreakerState())
}

func TestClose_ReportsRecordsThatCouldNotBeDelivered(t *testing.T) {
	resilience.ResetCircuitBreakers()
	t.Cleanup(resilience.ResetCircuitBreakers)
	logger := useRecordingLogger(t)
	client, err := NewMeteringClient(MeteringClientConfig{APIKey: "test-key", BaseURL: unreachableBaseURL()})
	require.NoError(t, err)
	client.retryConfig = testRetryConfig

	for i := 0; i < 3; i++ {
		client.Send(NewPayload(OperationChat, "gpt-4", "OPENAI").Build())
	}
	err = client.Close()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "3 usage records were not delivered")
	var closeWarnings []string
	for _, line := range logger.Lines() {
		if strings.HasPrefix(line, "WARN [METERING] 3 usage records could not be delivered") {
			closeWarnings = append(closeWarnings, line)
		}
	}
	assert.Len(t, closeWarnings, 1)
}

func TestClose_ReturnsNilWhenEverythingWasDelivered(t *testing.T) {
	srv, hits := countingServer(t)
	client, err := NewMeteringClient(MeteringClientConfig{APIKey: "test-key", BaseURL: srv.URL})
	require.NoError(t, err)

	client.Send(NewPayload(OperationChat, "gpt-4", "OPENAI").Build())

	require.NoError(t, client.Close())
	assert.Equal(t, int32(1), hits.Load())
}

func TestClose_NeverReportsSuccessWhileASendThatFailsDuringTheCountIsHeld(t *testing.T) {
	resilience.ResetCircuitBreakers()
	t.Cleanup(resilience.ResetCircuitBreakers)
	release := make(chan struct{})
	var releaseOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		<-release
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(srv.Close)
	client := newTestClient(t, srv.URL)
	client.retryConfig = &resilience.RetryConfig{MaxRetries: 0}
	client.buffer.flushTimeout = 20 * time.Millisecond
	sendDone := make(chan error, 1)
	go func() { sendDone <- client.SendSync(NewPayload(OperationChat, "gpt-4", "OPENAI").Build()) }()
	require.Eventually(t, func() bool { return client.sends.running() == 1 }, time.Second, time.Millisecond)
	original := afterSamplingRunningSends
	afterSamplingRunningSends = func() {
		releaseOnce.Do(func() { close(release) })
		<-sendDone
	}
	t.Cleanup(func() { afterSamplingRunningSends = original })

	err := client.Close()

	require.Error(t, err, "the record the send held while Close was counting is reported")
	assert.Equal(t, 1, client.GetBufferStats().Size)
}
