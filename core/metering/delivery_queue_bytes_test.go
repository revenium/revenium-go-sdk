package metering

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/revenium/revenium-go-sdk/core/resilience"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	queueByteBudget   = 200000
	slowQueueWorkers  = 2
	slowQueueCapacity = 50
	fullBufferWarning = "buffer full"
)

func captureSizedRecord() *MeteringPayload {
	return NewPayload(OperationChat, "gpt-4", "OPENAI").
		WithAttributes(map[string]interface{}{"capturedText": strings.Repeat("x", 20000)}).
		Build()
}

func encodedSize(t *testing.T, payload *MeteringPayload) int {
	t.Helper()
	body, err := json.Marshal(payload)
	require.NoError(t, err)
	return len(body)
}

func newStalledQueueClient(t *testing.T, apiKey string) (*MeteringClient, *loadStub, func()) {
	t.Helper()
	resilience.ResetCircuitBreakers()
	t.Cleanup(resilience.ResetCircuitBreakers)
	release := make(chan struct{})
	stub := newLoadStub(t, func(w http.ResponseWriter, _ *http.Request) {
		<-release
		w.WriteHeader(http.StatusCreated)
	})
	client, err := NewMeteringClient(MeteringClientConfig{
		APIKey:         apiKey,
		BaseURL:        stub.URL,
		BufferMaxBytes: queueByteBudget,
		RequestTimeout: time.Minute,
	})
	require.NoError(t, err)
	client.retryConfig = fastRetries
	client.pool = newDeliveryPool(slowQueueWorkers, slowQueueCapacity)
	released := false
	unstall := func() {
		if !released {
			released = true
			close(release)
		}
	}
	t.Cleanup(func() {
		unstall()
		<-client.sends.finished()
		_ = client.Close()
	})
	return client, stub, unstall
}

// recordsHeld counts every record of the client kept in memory: waiting in
// the delivery queue, being sent, queued for the next resend or being resent.
func recordsHeld(client *MeteringClient, stub *loadStub) int {
	stub.mu.Lock()
	sending := stub.inFlight
	stub.mu.Unlock()
	stats := client.GetBufferStats()
	return len(client.pool.jobs) + sending + stats.Size + stats.InFlight
}

func TestSend_SlowReveniumKeepsQueuedAndHeldRecordsInsideTheByteBudget(t *testing.T) {
	logger := useRecordingLogger(t)
	client, stub, _ := newStalledQueueClient(t, "hak_slow_queue_bytes")
	recordSize := encodedSize(t, captureSizedRecord())

	for i := 0; i < 60; i++ {
		client.Send(captureSizedRecord())
	}
	require.Eventually(t, func() bool { _, _, _, inFlight := stub.counts(); return inFlight == slowQueueWorkers }, 2*time.Second, time.Millisecond)

	held := recordsHeld(client, stub)
	assert.LessOrEqual(t, held*recordSize, queueByteBudget+recordSize, "%d records of %d bytes are held", held, recordSize)
	assert.LessOrEqual(t, client.GetBufferStats().HeldBytes, int64(queueByteBudget))
	assert.Equal(t, 1, countLinesContaining(logger, fullBufferWarning), "one warning for the whole stall")
}

func TestSend_RecordsWaitingToBeSentAreDeliveredOnceASlowReveniumAnswers(t *testing.T) {
	client, stub, unstall := newStalledQueueClient(t, "hak_slow_queue_delivers")

	for i := 0; i < 5; i++ {
		client.Send(captureSizedRecord())
	}
	unstall()
	<-client.sends.finished()

	requests, _, _, _ := stub.counts()
	assert.Equal(t, 5, requests, "records inside the budget are all sent")
	assert.Zero(t, client.GetBufferStats().HeldBytes, "every reservation is returned")
}

func TestBuffer_SendReservationsShareTheOutageWarningAndReArmItOnceTheQueueDrains(t *testing.T) {
	logger := useRecordingLogger(t)
	buf := newMeteringBuffer(bufferLimits{bytes: 1000}, time.Hour, time.Second, newDeliveryPath(newTestBreaker(), defaultSender))

	var reserved []*sendReservation
	for i := 0; i < 5; i++ {
		if reservation := buf.reserveForSend(300); reservation != nil {
			reserved = append(reserved, reservation)
		}
	}
	require.Len(t, reserved, 3)
	assert.Equal(t, 1, countLinesContaining(logger, fullBufferWarning))
	assert.Equal(t, int64(2), buf.GetBufferStats().EventsEvicted)

	reserved[0].release()
	assert.Nil(t, buf.reserveForSend(700), "two reservations still fill more than half the limit")
	assert.Equal(t, 1, countLinesContaining(logger, fullBufferWarning), "still the same outage")

	reserved[1].release()
	reserved[2].release()
	require.Zero(t, buf.GetBufferStats().HeldBytes)
	for i := 0; i < 5; i++ {
		buf.reserveForSend(300)
	}
	assert.Equal(t, 2, countLinesContaining(logger, fullBufferWarning), "a new stall warns again")
}

func TestBuffer_ASendReservationPushesOutTheOldestHeldRecords(t *testing.T) {
	useRecordingLogger(t)
	buf := newMeteringBuffer(bufferLimits{bytes: 1000}, time.Hour, time.Second, newDeliveryPath(newTestBreaker(), defaultSender))
	defer buf.Stop()
	for i := 0; i < 3; i++ {
		buf.Push(BufferedEvent{URL: unreachableBaseURL(), Body: []byte(strings.Repeat("a", 290))})
	}

	require.NotNil(t, buf.reserveForSend(500))

	stats := buf.GetBufferStats()
	assert.Equal(t, 1, stats.Size, "the two oldest held records make room for the live one")
	assert.LessOrEqual(t, stats.HeldBytes, int64(1000))
}

func TestBuffer_ASendReservationIsReturnedOnlyOnce(t *testing.T) {
	buf := newMeteringBuffer(bufferLimits{bytes: 1000}, time.Hour, time.Second, newDeliveryPath(newTestBreaker(), defaultSender))
	first := buf.reserveForSend(300)
	require.NotNil(t, buf.reserveForSend(300))

	first.release()
	first.release()

	assert.Equal(t, int64(300), buf.GetBufferStats().HeldBytes, "the second release of the same reservation returns nothing")
}

func TestSend_APanicWhileDeliveringReturnsTheRecordsByteReservation(t *testing.T) {
	useRecordingLogger(t)
	resilience.ResetCircuitBreakers()
	t.Cleanup(resilience.ResetCircuitBreakers)
	stub := newLoadStub(t, status(http.StatusCreated))
	recordSize := encodedSize(t, captureSizedRecord())
	client, err := NewMeteringClient(MeteringClientConfig{
		APIKey:         "hak_panicking_delivery",
		BaseURL:        stub.URL,
		BufferMaxBytes: int64(recordSize * 3 / 2),
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })
	client.retryConfig = fastRetries
	panicking := recordKind{label: "metering", noun: "payload", onDelivered: func([]byte) { panic("summary printer failed") }}

	record, err := client.encodeUsageRecord(captureSizedRecord())
	require.NoError(t, err)
	client.enqueue(record, panicking)
	<-client.sends.finished()
	require.Zero(t, client.GetBufferStats().HeldBytes, "the panicking send gave its bytes back")

	client.Send(captureSizedRecord())
	<-client.sends.finished()

	requests, _, _, _ := stub.counts()
	assert.Equal(t, 2, requests, "the record after the panic still fits the budget and is sent")
	assert.Zero(t, client.GetBufferStats().EventsEvicted)
}

func TestSend_AFailedRecordIsHeldWithoutLeavingTheByteBudgetWhileOthersAreQueued(t *testing.T) {
	useRecordingLogger(t)
	resilience.ResetCircuitBreakers()
	t.Cleanup(resilience.ResetCircuitBreakers)
	release := make(chan struct{})
	stub := newLoadStub(t, func(w http.ResponseWriter, _ *http.Request) {
		<-release
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	recordSize := encodedSize(t, captureSizedRecord())
	client, err := NewMeteringClient(MeteringClientConfig{
		APIKey:              "hak_failed_record_transfer",
		BaseURL:             stub.URL,
		BufferMaxBytes:      int64(recordSize * 8),
		BufferFlushInterval: time.Hour,
		RequestTimeout:      time.Minute,
	})
	require.NoError(t, err)
	client.retryConfig = &resilience.RetryConfig{MaxRetries: 0}
	client.pool = newDeliveryPool(4, 8)
	t.Cleanup(func() {
		<-client.sends.finished()
		_ = client.Close()
	})
	for i := 0; i < 8; i++ {
		client.Send(captureSizedRecord())
	}

	var senders sync.WaitGroup
	for g := 0; g < 8; g++ {
		senders.Add(1)
		go func() {
			defer senders.Done()
			for i := 0; i < 50; i++ {
				client.Send(captureSizedRecord())
			}
		}()
	}
	close(release)
	senders.Wait()
	<-client.sends.finished()

	assert.False(t, client.buffer.resendFull.Load(), "no failed record was dropped as the newest for want of room while nothing was being resent")
	assert.LessOrEqual(t, client.GetBufferStats().HeldBytes, int64(recordSize*8))
}
