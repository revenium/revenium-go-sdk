package metering

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/revenium/revenium-go-sdk/core/jobs"
	"github.com/revenium/revenium-go-sdk/core/resilience"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var fastRetries = &resilience.RetryConfig{MaxRetries: 3, BaseDelay: time.Millisecond, MaxDelay: 10 * time.Millisecond}

type loadStub struct {
	*httptest.Server
	mu          sync.Mutex
	openConns   int
	peakConns   int
	inFlight    int
	peakFlight  int
	requests    int
	distinct    map[string]int
	respond     func(w http.ResponseWriter, r *http.Request)
	requestedAt []time.Time
}

func newLoadStub(t *testing.T, respond func(w http.ResponseWriter, r *http.Request)) *loadStub {
	t.Helper()
	s := &loadStub{distinct: map[string]int{}, respond: respond}
	s.Server = httptest.NewUnstartedServer(http.HandlerFunc(s.handle))
	s.Server.Config.ConnState = s.trackConn
	s.Start()
	t.Cleanup(s.Close)
	return s
}

func (s *loadStub) trackConn(_ net.Conn, state http.ConnState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch state {
	case http.StateNew:
		s.openConns++
		s.peakConns = max(s.peakConns, s.openConns)
	case http.StateClosed, http.StateHijacked:
		s.openConns--
	}
}

func (s *loadStub) handle(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.requests++
	s.distinct[r.Header.Get("Idempotency-Key")]++
	s.requestedAt = append(s.requestedAt, time.Now())
	s.inFlight++
	s.peakFlight = max(s.peakFlight, s.inFlight)
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.inFlight--
		s.mu.Unlock()
	}()
	s.respond(w, r)
}

func (s *loadStub) counts() (requests, distinct, peakConns, peakFlight int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.requests, len(s.distinct), s.peakConns, s.peakFlight
}

func (s *loadStub) times() []time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]time.Time(nil), s.requestedAt...)
}

func status(code int) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(code) }
}

func newLoadClient(t *testing.T, baseURL, apiKey string) *MeteringClient {
	t.Helper()
	client, err := NewMeteringClient(MeteringClientConfig{APIKey: apiKey, BaseURL: baseURL})
	require.NoError(t, err)
	client.retryConfig = fastRetries
	return client
}

func TestSend_SlowReveniumNeverGetsMoreConnectionsThanTheCap(t *testing.T) {
	resilience.ResetCircuitBreakers()
	t.Cleanup(resilience.ResetCircuitBreakers)
	stub := newLoadStub(t, func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusCreated)
	})
	client := newLoadClient(t, stub.URL, "hak_load_key")

	for i := 0; i < 300; i++ {
		client.Send(NewPayload(OperationChat, "gpt-4", "OPENAI").Build())
	}
	require.NoError(t, client.Close())

	requests, distinct, peakConns, peakFlight := stub.counts()
	assert.Equal(t, 300, distinct, "every record is delivered")
	assert.Equal(t, 300, requests, "and each one exactly once")
	assert.LessOrEqual(t, peakConns, core.ReveniumMaxConnsPerHost)
	assert.LessOrEqual(t, peakFlight, deliveryWorkers)
}

func TestSend_SustainedOutageCostsAboutOneRequestPerRecordBeforeTheBreakerOpens(t *testing.T) {
	for _, code := range []int{http.StatusServiceUnavailable, http.StatusTooManyRequests} {
		t.Run(http.StatusText(code), func(t *testing.T) {
			resilience.ResetCircuitBreakers()
			t.Cleanup(resilience.ResetCircuitBreakers)
			stub := newLoadStub(t, status(code))
			client := newLoadClient(t, stub.URL, "hak_outage_key")

			for i := 0; i < 200; i++ {
				client.Send(NewPayload(OperationChat, "gpt-4", "OPENAI").Build())
			}
			client.Flush()

			requests, distinct, _, _ := stub.counts()
			require.Greater(t, distinct, 0)
			assert.LessOrEqual(t, float64(requests)/float64(distinct), 1.5, "requests=%d distinct=%d", requests, distinct)
			assert.Equal(t, resilience.StateOpen, client.CircuitBreakerState())
			assert.Equal(t, 200, client.buffer.Undelivered(), "nothing is lost while Revenium is down")
		})
	}
}

func TestSendSync_WaitsOutAShortRetryAfter(t *testing.T) {
	resilience.ResetCircuitBreakers()
	t.Cleanup(resilience.ResetCircuitBreakers)
	var calls atomic.Int32
	stub := newLoadStub(t, func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusCreated)
	})
	client := newLoadClient(t, stub.URL, "hak_retry_after_key")
	client.retryConfig = &resilience.RetryConfig{MaxRetries: 3, BaseDelay: time.Millisecond, MaxDelay: 5 * time.Second}

	require.NoError(t, client.SendSync(NewPayload(OperationChat, "gpt-4", "OPENAI").Build()))

	times := stub.times()
	require.Len(t, times, 2)
	assert.GreaterOrEqual(t, times[1].Sub(times[0]), time.Second)
	assert.Equal(t, 0, client.GetBufferStats().Size)
}

func TestSend_LongRetryAfterHoldsEveryRecordForThatKeyUntilItPasses(t *testing.T) {
	resilience.ResetCircuitBreakers()
	t.Cleanup(resilience.ResetCircuitBreakers)
	var throttled atomic.Bool
	throttled.Store(true)
	stub := newLoadStub(t, func(w http.ResponseWriter, _ *http.Request) {
		if throttled.Swap(false) {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusCreated)
	})
	client := newLoadClient(t, stub.URL, "hak_retry_after_long")

	require.Error(t, client.SendSync(NewPayload(OperationChat, "gpt-4", "OPENAI").Build()))
	for i := 0; i < 5; i++ {
		require.ErrorIs(t, client.SendSync(NewPayload(OperationChat, "gpt-4", "OPENAI").Build()), resilience.ErrCircuitOpen)
	}
	requests, _, _, _ := stub.counts()
	assert.Equal(t, 1, requests, "nothing is sent for this key while Revenium asked to wait")
	assert.Equal(t, 6, client.GetBufferStats().Size)

	time.Sleep(1100 * time.Millisecond)
	client.Flush()

	assert.Equal(t, 0, client.GetBufferStats().Size)
	requests, distinct, _, _ := stub.counts()
	assert.Equal(t, 7, requests)
	assert.Equal(t, 6, distinct)
}

func TestSend_OneThrottledKeyDoesNotHoldBackAnotherKeysRecordsOrJobCalls(t *testing.T) {
	resilience.ResetCircuitBreakers()
	t.Cleanup(resilience.ResetCircuitBreakers)
	var keyBHits atomic.Int32
	stub := newLoadStub(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") == "hak_customer_a" {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		keyBHits.Add(1)
		if strings.HasPrefix(r.URL.Path, "/profitstream/") {
			_, _ = w.Write([]byte(`["CHAT"]`))
			return
		}
		w.WriteHeader(http.StatusCreated)
	})
	clientA := newLoadClient(t, stub.URL, "hak_customer_a")
	clientB := newLoadClient(t, stub.URL, "hak_customer_b")
	jobsB, err := jobs.NewJobClient(jobs.JobClientConfig{APIKey: "hak_customer_b", BaseURL: stub.URL, TeamID: "team-b"})
	require.NoError(t, err)

	for i := 0; i < 10; i++ {
		_ = clientA.SendSync(NewPayload(OperationChat, "gpt-4", "OPENAI").Build())
	}
	require.Equal(t, resilience.StateOpen, clientA.CircuitBreakerState())

	require.NoError(t, clientB.SendSync(NewPayload(OperationChat, "gpt-4", "OPENAI").Build()))
	types, err := jobsB.GetJobTypes()
	require.NoError(t, err)
	assert.Equal(t, []string{"CHAT"}, types)
	assert.Equal(t, int32(2), keyBHits.Load())
	assert.Equal(t, 0, clientB.GetBufferStats().Size)
}

func TestSend_RecordsBeyondTheQueueAreHeldAndResentNotDropped(t *testing.T) {
	resilience.ResetCircuitBreakers()
	t.Cleanup(resilience.ResetCircuitBreakers)
	logger := useRecordingLogger(t)
	release := make(chan struct{})
	stub := newLoadStub(t, func(w http.ResponseWriter, _ *http.Request) {
		<-release
		w.WriteHeader(http.StatusCreated)
	})
	client := newLoadClient(t, stub.URL, "hak_overflow_key")
	client.pool = newDeliveryPool(1, 1)

	client.Send(NewPayload(OperationChat, "gpt-4", "OPENAI").Build())
	require.Eventually(t, func() bool { _, _, _, inFlight := stub.counts(); return inFlight == 1 }, time.Second, time.Millisecond)
	for i := 0; i < 4; i++ {
		client.Send(NewPayload(OperationChat, "gpt-4", "OPENAI").Build())
	}
	assert.Equal(t, 3, client.GetBufferStats().Size, "one record is being sent, one waits, three are held")
	close(release)
	require.NoError(t, client.Close())

	_, distinct, _, _ := stub.counts()
	assert.Equal(t, 5, distinct)
	var overflowWarnings int
	for _, line := range logger.Lines() {
		if strings.Contains(line, "arriving faster than Revenium accepts them") {
			overflowWarnings++
		}
	}
	assert.Equal(t, 1, overflowWarnings)
}

func TestDeliveryRecovery_ResendsHeldRecordsWithoutWaitingForTheTimer(t *testing.T) {
	resilience.ResetCircuitBreakers()
	t.Cleanup(resilience.ResetCircuitBreakers)
	var up atomic.Bool
	stub := newLoadStub(t, func(w http.ResponseWriter, _ *http.Request) {
		if !up.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusCreated)
	})
	client := newLoadClient(t, stub.URL, "hak_recovery_key")
	client.path.breaker = newTestBreaker()
	t.Cleanup(func() { _ = client.Close() })

	require.Error(t, client.SendSync(NewPayload(OperationChat, "gpt-4", "OPENAI").Build()))
	require.Equal(t, 1, client.GetBufferStats().Size)
	up.Store(true)
	require.NoError(t, client.SendSync(NewPayload(OperationChat, "gpt-4", "OPENAI").Build()))

	require.Eventually(t, func() bool { return client.GetBufferStats().EventsReplayed == 1 }, 2*time.Second, 10*time.Millisecond)
	assert.Equal(t, 0, client.buffer.Undelivered())
}

func TestClose_ReturnsInBoundedTimeAndCountsRecordsStillQueuedWhenReveniumIsSlow(t *testing.T) {
	resilience.ResetCircuitBreakers()
	t.Cleanup(resilience.ResetCircuitBreakers)
	release := make(chan struct{})
	stub := newLoadStub(t, func(w http.ResponseWriter, _ *http.Request) {
		<-release
		w.WriteHeader(http.StatusCreated)
	})
	client := newLoadClient(t, stub.URL, "hak_slow_close_key")
	t.Cleanup(func() {
		close(release)
		<-client.sends.finished()
	})
	client.pool = newDeliveryPool(1, 10)
	client.buffer.flushTimeout = 100 * time.Millisecond

	for i := 0; i < 5; i++ {
		client.Send(NewPayload(OperationChat, "gpt-4", "OPENAI").Build())
	}
	start := time.Now()
	err := client.Close()

	assert.Less(t, time.Since(start), 2*time.Second)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "5 usage records were not delivered")
}
