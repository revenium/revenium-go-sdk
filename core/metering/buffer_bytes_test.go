package metering

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const captureFieldSize = 50000

type switchableRevenium struct {
	server   *httptest.Server
	down     atomic.Bool
	mu       sync.Mutex
	accepted []string
}

func newSwitchableRevenium(t *testing.T) *switchableRevenium {
	t.Helper()
	r := &switchableRevenium{}
	r.down.Store(true)
	r.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if r.down.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		r.mu.Lock()
		r.accepted = append(r.accepted, req.Header.Get("Idempotency-Key"))
		r.mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(r.server.Close)
	return r
}

func (r *switchableRevenium) acceptedKeys() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.accepted...)
}

func capturedRecordBody(t *testing.T, key string) []byte {
	t.Helper()
	field := strings.Repeat("x", captureFieldSize)
	body, err := json.Marshal(&MeteringPayload{
		TransactionID:  key,
		Model:          "gpt-4.1-nano",
		Provider:       "OPENAI",
		SystemPrompt:   field,
		InputMessages:  field,
		OutputResponse: field,
	})
	require.NoError(t, err)
	return body
}

func capturedRecord(t *testing.T, url, key string) BufferedEvent {
	t.Helper()
	return BufferedEvent{
		URL:            url,
		Headers:        map[string]string{"Content-Type": "application/json", "Idempotency-Key": key},
		Body:           capturedRecordBody(t, key),
		IdempotencyKey: key,
	}
}

func measuredBytes(events []BufferedEvent) int {
	total := 0
	for _, e := range events {
		total += len(e.Body) + len(e.URL) + len(e.IdempotencyKey)
		for name, value := range e.Headers {
			total += len(name) + len(value)
		}
	}
	return total
}

func measuredQueuedBytes(b *MeteringBuffer) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return measuredBytes(b.events)
}

func assertHeldBytesAccounted(t *testing.T, b *MeteringBuffer, inFlight []BufferedEvent) {
	t.Helper()
	b.mu.Lock()
	queued := len(b.events)
	measuredQueued := int64(measuredBytes(b.events))
	assert.Equal(t, measuredQueued, b.queuedBytes, "queuedBytes == the bytes of the queued records")
	assert.Equal(t, int64(measuredBytes(inFlight)), b.inFlightBytes, "inFlightBytes == the bytes of the records being resent")
	b.mu.Unlock()

	stats := b.GetBufferStats()
	assert.Equal(t, queued, stats.Size)
	assert.Equal(t, len(inFlight), stats.InFlight)
	assert.Equal(t, measuredQueued+int64(measuredBytes(inFlight)), stats.HeldBytes)
}

func heldKeys(b *MeteringBuffer) []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	keys := make([]string, 0, len(b.events))
	for _, e := range b.events {
		keys = append(keys, e.IdempotencyKey)
	}
	return keys
}

func countFullWarnings(logger *recordingLogger) int {
	return countLinesContaining(logger, "buffer full")
}

func keyRange(from, to int) []string {
	keys := make([]string, 0, to-from)
	for i := from; i < to; i++ {
		keys = append(keys, "k"+strconv.Itoa(i))
	}
	return keys
}

func TestBuffer_CaptureSizedRecordsStayUnderTheByteBudget(t *testing.T) {
	const budget = 1_000_000
	t.Setenv(core.EnvBufferMaxBytes, strconv.Itoa(budget))
	buf := NewMeteringBuffer(20000, time.Hour, 5*time.Second)
	defer buf.Stop()

	recordSize := len(capturedRecord(t, "http://localhost/test", "k00").Body)
	require.Greater(t, recordSize, 3*captureFieldSize)
	for i := 0; i < 40; i++ {
		buf.Push(capturedRecord(t, "http://localhost/test", "k"+strconv.Itoa(i)))
	}

	held := measuredQueuedBytes(buf)
	assert.LessOrEqual(t, held, budget)
	assert.Greater(t, held, budget-2*recordSize, "the budget is filled, not left mostly empty")
	kept := heldKeys(buf)
	assert.Equal(t, keyRange(40-len(kept), 40), kept, "the newest records are the ones kept")
	assert.Equal(t, int64(40-len(kept)), buf.GetBufferStats().EventsEvicted)
}

func TestBuffer_RecordCountStillCapsSmallRecordsUnderTheByteBudget(t *testing.T) {
	t.Setenv(core.EnvBufferMaxBytes, "1000000")
	buf := NewMeteringBuffer(5, time.Hour, 5*time.Second)
	defer buf.Stop()

	for i := 0; i < 20; i++ {
		buf.Push(makeEvent("k" + strconv.Itoa(i)))
	}

	assert.Equal(t, keyRange(15, 20), heldKeys(buf))
	assert.Equal(t, int64(15), buf.GetBufferStats().EventsEvicted)
}

func TestBuffer_RecordLargerThanTheWholeBudgetIsDroppedAndKeepsTheOthers(t *testing.T) {
	t.Setenv(core.EnvBufferMaxBytes, "100000")
	buf := NewMeteringBuffer(100, time.Hour, 5*time.Second)
	defer buf.Stop()

	buf.Push(makeEvent("small-1"))
	buf.Push(capturedRecord(t, "http://localhost/test", "huge"))
	buf.Push(makeEvent("small-2"))

	assert.Equal(t, []string{"small-1", "small-2"}, heldKeys(buf))
	assert.Equal(t, int64(1), buf.GetBufferStats().EventsEvicted)
}

func TestBuffer_ByteBudgetWarnsOncePerOutage(t *testing.T) {
	logger := useRecordingLogger(t)
	t.Setenv(core.EnvBufferMaxBytes, "1000000")
	revenium := newSwitchableRevenium(t)
	buf := NewMeteringBuffer(20000, time.Hour, 5*time.Second)
	defer buf.Stop()
	buf.path.monitor.onRecovery = nil
	buf.path.breaker = newTestBreaker()

	for round := 0; round < 3; round++ {
		for i := 0; i < 10; i++ {
			buf.Push(capturedRecord(t, revenium.server.URL, "r"+strconv.Itoa(round)+"-"+strconv.Itoa(i)))
		}
		buf.flush()
	}
	assert.Equal(t, 1, countFullWarnings(logger), "one warning for the whole outage")

	revenium.down.Store(false)
	buf.flush()
	revenium.down.Store(true)
	for i := 0; i < 10; i++ {
		buf.Push(capturedRecord(t, revenium.server.URL, "next-"+strconv.Itoa(i)))
	}
	assert.Equal(t, 2, countFullWarnings(logger), "a later outage warns again")
}

func TestBuffer_DrainAfterReconnectSendsTheRecordsKeptUnderTheByteBudget(t *testing.T) {
	t.Setenv(core.EnvBufferMaxBytes, "1000000")
	revenium := newSwitchableRevenium(t)
	buf := NewMeteringBuffer(20000, time.Hour, 5*time.Second)
	defer buf.Stop()
	buf.path.monitor.onRecovery = nil
	buf.path.breaker = newTestBreaker()

	for i := 0; i < 20; i++ {
		buf.Push(capturedRecord(t, revenium.server.URL, "k"+strconv.Itoa(i)))
	}
	buf.flush()
	kept := heldKeys(buf)
	require.NotEmpty(t, kept)
	require.Less(t, len(kept), 20)

	revenium.down.Store(false)
	buf.flush()

	assert.ElementsMatch(t, kept, revenium.acceptedKeys())
	assert.Empty(t, heldKeys(buf))
	assert.Equal(t, 0, measuredQueuedBytes(buf))
}

func TestBuffer_RecordCountCapWarnsOncePerOutageAcrossFlushes(t *testing.T) {
	logger := useRecordingLogger(t)
	revenium := newSwitchableRevenium(t)
	buf := NewMeteringBuffer(3, time.Hour, 5*time.Second)
	defer buf.Stop()
	buf.path.monitor.onRecovery = nil
	buf.path.breaker = newTestBreaker()

	for round := 0; round < 3; round++ {
		for i := 0; i < 10; i++ {
			event := makeEvent("r" + strconv.Itoa(round) + "-" + strconv.Itoa(i))
			event.URL = revenium.server.URL
			buf.Push(event)
		}
		buf.flush()
	}

	assert.Equal(t, 1, countFullWarnings(logger))
}

func countLinesContaining(logger *recordingLogger, text string) int {
	count := 0
	for _, line := range logger.Lines() {
		if strings.Contains(line, text) {
			count++
		}
	}
	return count
}

func TestBuffer_OversizeDropNamesTheRecordSizeAndLeavesTheOutageWarningArmed(t *testing.T) {
	logger := useRecordingLogger(t)
	t.Setenv(core.EnvBufferMaxBytes, "100000")
	buf := NewMeteringBuffer(3, time.Hour, 5*time.Second)
	defer buf.Stop()

	huge := capturedRecord(t, "http://localhost/test", "huge")
	buf.Push(huge)
	assert.Equal(t, 1, countLinesContaining(logger, "dropped a usage record of "+strconv.FormatInt(huge.heldBytes(), 10)+" bytes, larger than REVENIUM_BUFFER_MAX_BYTES"))
	assert.Equal(t, 0, countFullWarnings(logger))

	for i := 0; i < 5; i++ {
		buf.Push(makeEvent("k" + strconv.Itoa(i)))
	}
	assert.Equal(t, 1, countFullWarnings(logger), "the outage after an oversize drop still warns")
}

func TestBuffer_OutageWarningRearmsAfterEveryHeldRecordExpires(t *testing.T) {
	logger := useRecordingLogger(t)
	buf := NewMeteringBuffer(3, time.Hour, 5*time.Second)
	defer buf.Stop()

	for i := 0; i < 5; i++ {
		buf.Push(makeEvent("first-" + strconv.Itoa(i)))
	}
	require.Equal(t, 1, countFullWarnings(logger))

	buf.mu.Lock()
	for i := range buf.events {
		buf.events[i].CreatedAt = time.Now().Add(-maxEventAge - time.Minute)
	}
	buf.mu.Unlock()
	buf.flush()
	require.Empty(t, heldKeys(buf))

	for i := 0; i < 5; i++ {
		buf.Push(makeEvent("second-" + strconv.Itoa(i)))
	}
	assert.Equal(t, 2, countFullWarnings(logger), "the next outage warns again")
}
