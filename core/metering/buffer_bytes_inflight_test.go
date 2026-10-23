package metering

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stallingRevenium struct {
	server  *httptest.Server
	stalled chan struct{}
	release chan struct{}
}

func newStallingRevenium(t *testing.T, answeredBeforeStall int32) *stallingRevenium {
	t.Helper()
	r := &stallingRevenium{stalled: make(chan struct{}), release: make(chan struct{})}
	var requests atomic.Int32
	var stallOnce sync.Once
	r.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if requests.Add(1) > answeredBeforeStall {
			stallOnce.Do(func() { close(r.stalled) })
			<-r.release
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(r.server.Close)
	return r
}

func (r *stallingRevenium) waitUntilStalled(t *testing.T) {
	t.Helper()
	select {
	case <-r.stalled:
	case <-time.After(5 * time.Second):
		t.Fatal("the flush never reached the stalled window")
	}
}

func newRecordHeldBuffer(t *testing.T, maxBytes int64) *MeteringBuffer {
	t.Helper()
	t.Setenv(core.EnvBufferMaxBytes, strconv.FormatInt(maxBytes, 10))
	buf := NewMeteringBuffer(20000, time.Hour, 5*time.Second)
	buf.path.monitor.onRecovery = nil
	buf.path.breaker = newTestBreaker()
	return buf
}

func TestBuffer_PushesDuringAStalledFlushKeepEveryHeldBodyInsideTheBudget(t *testing.T) {
	revenium := newStallingRevenium(t, 1)
	recordSize := capturedRecord(t, revenium.server.URL, "k00").heldBytes()
	budget := 10*recordSize + recordSize/2
	buf := newRecordHeldBuffer(t, budget)
	defer buf.Stop()

	for i := 0; i < 10; i++ {
		buf.Push(capturedRecord(t, revenium.server.URL, fmt.Sprintf("k%02d", i)))
	}
	pending := buf.takePending()
	require.Len(t, pending, 10)
	replayed := make(chan []BufferedEvent, 1)
	go func() { replayed <- buf.replay(pending) }()
	revenium.waitUntilStalled(t)

	var pushes sync.WaitGroup
	for i := 0; i < 5; i++ {
		record := capturedRecord(t, revenium.server.URL, fmt.Sprintf("n%02d", i))
		pushes.Add(1)
		go func() {
			defer pushes.Done()
			buf.Push(record)
		}()
	}
	pushes.Wait()

	stillReferenced := int64(measuredQueuedBytes(buf)) + totalHeldBytes(pending)
	close(revenium.release)
	buf.putBack(<-replayed)
	assert.LessOrEqual(t, stillReferenced, budget, "a delivered body is still reachable after its budget was spent again")
	assert.Equal(t, int64(10), buf.GetBufferStats().EventsReplayed)
	assertHeldBytesAccounted(t, buf, nil)
}

func TestBuffer_StatsDuringAStalledFlushCountTheResentRecordsApartFromTheQueue(t *testing.T) {
	revenium := newStallingRevenium(t, 0)
	buf := newRecordHeldBuffer(t, 1000000)
	defer buf.Stop()

	var taken []BufferedEvent
	for i := 0; i < 3; i++ {
		record := capturedRecord(t, revenium.server.URL, fmt.Sprintf("k%02d", i))
		taken = append(taken, record)
		buf.Push(record)
	}
	flushed := make(chan struct{})
	go func() {
		defer close(flushed)
		buf.flush()
	}()
	revenium.waitUntilStalled(t)

	stats := buf.GetBufferStats()
	assert.Equal(t, 0, stats.Size)
	assert.Equal(t, 3, stats.InFlight)
	assertHeldBytesAccounted(t, buf, taken)

	close(revenium.release)
	<-flushed
	assertHeldBytesAccounted(t, buf, nil)
}

func TestBuffer_RecordsRejectedDuringAStalledResendWarnOnce(t *testing.T) {
	logger := useRecordingLogger(t)
	revenium := newStallingRevenium(t, 0)
	first := capturedRecord(t, revenium.server.URL, "k00")
	buf := newRecordHeldBuffer(t, first.heldBytes()+1)
	defer buf.Stop()
	buf.Push(first)

	flushed := make(chan struct{})
	go func() {
		defer close(flushed)
		buf.flush()
	}()
	revenium.waitUntilStalled(t)

	for i := 1; i <= 5; i++ {
		buf.Push(capturedRecord(t, revenium.server.URL, fmt.Sprintf("k%02d", i)))
	}
	assert.Equal(t, 1, countLinesContaining(logger, "dropping new usage records because the records being sent or resent fill"))
	assert.Equal(t, 0, countFullWarnings(logger))

	close(revenium.release)
	<-flushed
	buf.flush()
	buf.Push(capturedRecord(t, revenium.server.URL, "k99"))
	assert.Equal(t, 1, countLinesContaining(logger, "dropping new usage records because the records being sent or resent fill"), "a record that fits after the resend is kept without a warning")
	assert.Equal(t, 1, buf.GetBufferStats().Size)
}

func TestBuffer_RecordsRejectedDuringEachFailedResendWarnAgain(t *testing.T) {
	logger := useRecordingLogger(t)
	stalls := make(chan chan struct{}, 1)
	var round, stalledInRound atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if stalledInRound.CompareAndSwap(round.Load()-1, round.Load()) {
			release := make(chan struct{})
			stalls <- release
			<-release
		}
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(server.Close)
	first := capturedRecord(t, server.URL, "k00")
	buf := newRecordHeldBuffer(t, first.heldBytes()+16)
	defer buf.Stop()
	buf.Push(first)

	const warning = "dropping new usage records because the records being sent or resent fill"
	for resend := int32(1); resend <= 2; resend++ {
		round.Store(resend)
		flushed := make(chan struct{})
		go func() {
			defer close(flushed)
			buf.flush()
		}()
		var release chan struct{}
		select {
		case release = <-stalls:
		case <-time.After(5 * time.Second):
			t.Fatalf("resend %d never reached the server", resend)
		}
		for i := 1; i <= 3; i++ {
			buf.Push(capturedRecord(t, server.URL, fmt.Sprintf("%d%02d", resend, i)))
		}
		assert.Equal(t, int(resend), countLinesContaining(logger, warning), "resend %d warns once more", resend)
		close(release)
		<-flushed
		require.Equal(t, 1, buf.GetBufferStats().Size, "the failed resend put its record back")
	}
}
