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

type inFlightGauge struct {
	mu      sync.Mutex
	current int
	peak    int
}

func (g *inFlightGauge) handler(delay time.Duration) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		g.mu.Lock()
		g.current++
		g.peak = max(g.peak, g.current)
		g.mu.Unlock()
		time.Sleep(delay)
		g.mu.Lock()
		g.current--
		g.mu.Unlock()
		w.WriteHeader(http.StatusCreated)
	}
}

func (g *inFlightGauge) Peak() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.peak
}

func TestDelivery_ResendsAndLiveSendsAcrossHostsShareOneProcessWideCap(t *testing.T) {
	resilience.ResetCircuitBreakers()
	t.Cleanup(resilience.ResetCircuitBreakers)
	gauge := &inFlightGauge{}
	var clients []*MeteringClient
	for i := 0; i < 2; i++ {
		srv := httptest.NewServer(gauge.handler(30 * time.Millisecond))
		t.Cleanup(srv.Close)
		client := newLoadClient(t, srv.URL, "hak_cap_key")
		for j := 0; j < 60; j++ {
			holdUsageRecord(t, client, NewPayload(OperationChat, "gpt-4", "OPENAI").Build())
		}
		clients = append(clients, client)
	}

	var wg sync.WaitGroup
	for _, client := range clients {
		wg.Add(1)
		go func(client *MeteringClient) {
			defer wg.Done()
			for j := 0; j < 60; j++ {
				client.Send(NewPayload(OperationChat, "gpt-4", "OPENAI").Build())
			}
			client.buffer.flush()
		}(client)
	}
	wg.Wait()
	for _, client := range clients {
		require.NoError(t, client.Close())
	}

	assert.LessOrEqual(t, gauge.Peak(), deliveryWorkers)
}

func TestUndelivered_CountsOnlyRecordsNotYetDeliveredWhileADrainIsRunning(t *testing.T) {
	var requests atomic.Int32
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if requests.Add(1) > 1+replayConcurrency {
			<-release
		}
		w.WriteHeader(http.StatusCreated)
	}))
	t.Cleanup(srv.Close)
	buf := newTestBuffer(100)
	for i := 0; i < 20; i++ {
		buf.Push(BufferedEvent{URL: srv.URL, Headers: map[string]string{}, Body: []byte(`{}`)})
	}

	done := make(chan struct{})
	go func() {
		buf.flush()
		close(done)
	}()

	require.Eventually(t, func() bool { return requests.Load() == 20 }, 2*time.Second, time.Millisecond)
	assert.Equal(t, 3, buf.Undelivered())
	close(release)
	<-done
	assert.Equal(t, 0, buf.Undelivered())
}

func TestFlush_APanicWhileResendingKeepsTheRecordAndTheProcess(t *testing.T) {
	logger := useRecordingLogger(t)
	buf := newTestBuffer(10)
	buf.path.sender = nil
	buf.Push(BufferedEvent{URL: "http://127.0.0.1:1/meter", Headers: map[string]string{}, Body: []byte(`{}`)})

	buf.flush()

	assert.Equal(t, 1, buf.GetBufferStats().Size)
	var panics int
	for _, line := range logger.Lines() {
		if strings.HasPrefix(line, "ERROR [BUFFER] panic while resending") {
			panics++
		}
	}
	assert.Equal(t, 1, panics)
}

func TestClose_ForgetsTheKeysBreakerSoALaterClientStartsClosed(t *testing.T) {
	resilience.ResetCircuitBreakers()
	t.Cleanup(resilience.ResetCircuitBreakers)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(srv.Close)
	first := newLoadClient(t, srv.URL, "hak_rotating_key")
	_ = first.SendSync(NewPayload(OperationChat, "gpt-4", "OPENAI").Build())
	require.Equal(t, resilience.StateOpen, first.CircuitBreakerState())
	_ = first.Close()

	second := newLoadClient(t, srv.URL, "hak_rotating_key")
	t.Cleanup(func() { _ = second.Close() })

	assert.Equal(t, resilience.StateClosed, second.CircuitBreakerState())
}

func TestSendSync_ASendThatWaitedForASlotRespectsABreakerThatOpenedMeanwhile(t *testing.T) {
	resilience.ResetCircuitBreakers()
	t.Cleanup(resilience.ResetCircuitBreakers)
	srv, hits := countingServer(t)
	client := newLoadClient(t, srv.URL, "hak_waiting_key")
	t.Cleanup(func() { _ = client.Close() })
	limit := resilience.NewConcurrencyLimit(1)
	client.path.sendLimit = limit
	holdSlot := make(chan struct{})
	slotHeld := make(chan struct{})
	go func() {
		_ = limit.Run(func() error {
			close(slotHeld)
			<-holdSlot
			return nil
		})
	}()
	<-slotHeld

	result := make(chan error, 1)
	go func() { result <- client.SendSync(NewPayload(OperationChat, "gpt-4", "OPENAI").Build()) }()
	require.Eventually(t, func() bool { return limit.Waiting() == 1 }, 2*time.Second, time.Millisecond)
	client.path.breaker.HoldOpenFor(time.Minute)
	close(holdSlot)

	assert.ErrorIs(t, <-result, resilience.ErrCircuitOpen)
	assert.Equal(t, int32(0), hits.Load())
}

func TestDeliveryPool_AWorkerSurvivesAPanickingJob(t *testing.T) {
	logger := useRecordingLogger(t)
	pool := newDeliveryPool(1, 2)
	ran := make(chan struct{})

	require.True(t, pool.trySubmit(func() { panic("boom") }))
	require.True(t, pool.trySubmit(func() { close(ran) }))

	select {
	case <-ran:
	case <-time.After(2 * time.Second):
		t.Fatal("the only worker died with the panicking job")
	}
	assert.Contains(t, strings.Join(logger.Lines(), "\n"), "ERROR [METERING] panic in delivery worker: boom")
}

func TestSend_NilRecordIsReportedWithoutPanickingEvenWhenTheQueueIsFull(t *testing.T) {
	logger := useRecordingLogger(t)
	srv, _ := countingServer(t)
	client := newLoadClient(t, srv.URL, "hak_nil_key")
	t.Cleanup(func() { _ = client.Close() })
	client.pool = newDeliveryPool(0, 0)

	assert.NotPanics(t, func() { client.Send(nil) })
	assert.Error(t, client.SendSync(nil))
	assert.Contains(t, strings.Join(logger.Lines(), "\n"), "usage record must not be nil")
}

func TestSend_OverloadWarningFiresOncePerEpisodeWhileSlotsFreeUpOneAtATime(t *testing.T) {
	resilience.ResetCircuitBreakers()
	t.Cleanup(resilience.ResetCircuitBreakers)
	logger := useRecordingLogger(t)
	release := make(chan struct{})
	stub := newLoadStub(t, func(w http.ResponseWriter, _ *http.Request) {
		<-release
		w.WriteHeader(http.StatusCreated)
	})
	client := newLoadClient(t, stub.URL, "hak_overload_key")
	client.pool = newDeliveryPool(1, 4)
	t.Cleanup(func() {
		close(release)
		<-client.sends.finished()
	})

	client.Send(NewPayload(OperationChat, "gpt-4", "OPENAI").Build())
	require.Eventually(t, func() bool { _, _, _, inFlight := stub.counts(); return inFlight == 1 }, time.Second, time.Millisecond)
	for i := 0; i < 5; i++ {
		client.Send(NewPayload(OperationChat, "gpt-4", "OPENAI").Build())
	}
	for i := 0; i < 3; i++ {
		release <- struct{}{}
		require.Eventually(t, func() bool { return len(client.pool.jobs) < 4 }, time.Second, time.Millisecond)
		client.Send(NewPayload(OperationChat, "gpt-4", "OPENAI").Build())
		client.Send(NewPayload(OperationChat, "gpt-4", "OPENAI").Build())
	}

	var warnings int
	for _, line := range logger.Lines() {
		if strings.Contains(line, "arriving faster than Revenium accepts them") {
			warnings++
		}
	}
	assert.Equal(t, 1, warnings)
}

func TestClose_KeepsTheKeysBreakerUntilSendsStillRunningFinish(t *testing.T) {
	resilience.ResetCircuitBreakers()
	t.Cleanup(resilience.ResetCircuitBreakers)
	release := make(chan struct{})
	stub := newLoadStub(t, func(w http.ResponseWriter, _ *http.Request) {
		<-release
		w.WriteHeader(http.StatusCreated)
	})
	first := newLoadClient(t, stub.URL, "hak_slow_close_breaker")
	first.buffer.flushTimeout = 50 * time.Millisecond
	first.Send(NewPayload(OperationChat, "gpt-4", "OPENAI").Build())
	require.Eventually(t, func() bool { _, _, _, inFlight := stub.counts(); return inFlight == 1 }, time.Second, time.Millisecond)

	require.Error(t, first.Close())
	second := newLoadClient(t, stub.URL, "hak_slow_close_breaker")
	t.Cleanup(func() { _ = second.Close() })
	assert.Same(t, first.path.breaker, second.path.breaker, "a send still running keeps the key's breaker registered")

	close(release)
	<-first.sends.finished()
	require.NoError(t, second.Close())
	require.Eventually(t, func() bool {
		fresh := newLoadClient(t, stub.URL, "hak_slow_close_breaker")
		defer func() { _ = fresh.Close() }()
		return fresh.path.breaker != first.path.breaker
	}, 2*time.Second, 10*time.Millisecond)
}

func TestSendToolEvent_NilEventIsRejectedTheSameWayAsANilUsageRecord(t *testing.T) {
	logger := useRecordingLogger(t)
	srv, hits := countingServer(t)
	client := newLoadClient(t, srv.URL, "hak_nil_tool_key")
	t.Cleanup(func() { _ = client.Close() })
	client.pool = newDeliveryPool(0, 0)

	assert.NotPanics(t, func() { client.SendToolEvent(nil) })
	assert.Equal(t, 0, client.GetBufferStats().Size, "a nil event is not queued or held")
	assert.Error(t, client.SendToolEventSync(nil))
	client.Flush()

	assert.Contains(t, strings.Join(logger.Lines(), "\n"), "tool event payload must not be nil")
	assert.Equal(t, int32(0), hits.Load())
	assert.Equal(t, 0, client.sends.running())
}

func TestClose_ASecondCloseWaitsForTheFirstClosesDrainBeforeReleasingTheBreaker(t *testing.T) {
	resilience.ResetCircuitBreakers()
	t.Cleanup(resilience.ResetCircuitBreakers)
	release := make(chan struct{})
	var releaseOnce sync.Once
	stub := newLoadStub(t, func(w http.ResponseWriter, _ *http.Request) {
		<-release
		w.WriteHeader(http.StatusCreated)
	})
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
	first := newLoadClient(t, stub.URL, "hak_double_close")
	first.buffer.flushTimeout = 50 * time.Millisecond
	holdUsageRecord(t, first, NewPayload(OperationChat, "gpt-4", "OPENAI").Build())

	require.Error(t, first.Close(), "the drain is still sending when the first Close gives up")
	require.Error(t, first.Close(), "a second Close sees the same drain still running")
	second := newLoadClient(t, stub.URL, "hak_double_close")
	t.Cleanup(func() { _ = second.Close() })

	assert.Same(t, first.path.breaker, second.path.breaker, "the breaker stays registered while the drain still sends")
}

func TestClose_WaitsForASendSyncStillRunningBeforeReleasingTheBreaker(t *testing.T) {
	resilience.ResetCircuitBreakers()
	t.Cleanup(resilience.ResetCircuitBreakers)
	release := make(chan struct{})
	var releaseOnce sync.Once
	stub := newLoadStub(t, func(w http.ResponseWriter, _ *http.Request) {
		<-release
		w.WriteHeader(http.StatusCreated)
	})
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
	first := newLoadClient(t, stub.URL, "hak_sync_close")
	first.buffer.flushTimeout = 50 * time.Millisecond
	done := make(chan error, 1)
	go func() { done <- first.SendSync(NewPayload(OperationChat, "gpt-4", "OPENAI").Build()) }()
	require.Eventually(t, func() bool { _, _, _, inFlight := stub.counts(); return inFlight == 1 }, time.Second, time.Millisecond)

	require.Error(t, first.Close(), "Close counts the SendSync still in progress")
	second := newLoadClient(t, stub.URL, "hak_sync_close")
	t.Cleanup(func() { _ = second.Close() })
	assert.Same(t, first.path.breaker, second.path.breaker)

	releaseOnce.Do(func() { close(release) })
	require.NoError(t, <-done)
}

func TestNewMeteringBuffer_ResendsThroughTheSharedSlotsAndABreaker(t *testing.T) {
	buf := NewMeteringBuffer(10, time.Hour, time.Second)
	t.Cleanup(buf.Stop)

	assert.Same(t, sharedSendSlots, buf.path.sendLimit)
	require.NotNil(t, buf.path.breaker)
	require.NotNil(t, buf.path.monitor)
}
