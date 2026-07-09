package metering

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/revenium/revenium-go-sdk/core/resilience"
)

const (
	defaultBufferMaxSize       = 1000
	defaultBufferFlushInterval = 30 * time.Second
	defaultBufferFlushTimeout  = 10 * time.Second
	maxEventAge                = 24 * time.Hour
	replayRequestTimeout       = 10 * time.Second
	maxReplayResponseBody      = 64 * 1024
)

type BufferedEvent struct {
	URL            string
	Headers        map[string]string
	Body           []byte
	IdempotencyKey string
	CreatedAt      time.Time
}

type BufferStats struct {
	Size           int
	Capacity       int
	EventsReplayed int64
	EventsEvicted  int64
}

type MeteringBuffer struct {
	events         []BufferedEvent
	maxSize        int
	flushInterval  time.Duration
	flushTimeout   time.Duration
	mu             sync.RWMutex
	flushMu        sync.Mutex
	stopCh         chan struct{}
	loopDone       chan struct{}
	startOnce      sync.Once
	eventsReplayed atomic.Int64
	eventsEvicted  atomic.Int64
}

func NewMeteringBuffer(maxSize int, flushInterval, flushTimeout time.Duration) *MeteringBuffer {
	if maxSize <= 0 {
		maxSize = defaultBufferMaxSize
	}
	if flushInterval <= 0 {
		flushInterval = defaultBufferFlushInterval
	}
	if flushTimeout <= 0 {
		flushTimeout = defaultBufferFlushTimeout
	}
	return &MeteringBuffer{
		events:        make([]BufferedEvent, 0, 64),
		maxSize:       maxSize,
		flushInterval: flushInterval,
		flushTimeout:  flushTimeout,
		stopCh:        make(chan struct{}),
		loopDone:      make(chan struct{}),
	}
}

func (b *MeteringBuffer) Push(event BufferedEvent) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if len(b.events) >= b.maxSize {
		core.Warn("[BUFFER] buffer full (%d/%d), evicting oldest event", len(b.events), b.maxSize)
		copy(b.events, b.events[1:])
		b.events[len(b.events)-1] = BufferedEvent{}
		b.events = b.events[:len(b.events)-1]
		b.eventsEvicted.Add(1)
	}
	event.CreatedAt = time.Now()
	b.events = append(b.events, event)
	core.Debug("[BUFFER] event buffered, depth: %d/%d", len(b.events), b.maxSize)

	b.startOnce.Do(func() {
		go b.replayLoop()
	})
}

func (b *MeteringBuffer) DrainWithTimeout(timeout time.Duration) {
	done := make(chan struct{})
	go func() {
		defer close(done)
		b.flush()
	}()
	select {
	case <-done:
	case <-time.After(timeout):
		core.Warn("[BUFFER] drain timed out after %s", timeout)
	}
}

func (b *MeteringBuffer) Stop() {
	select {
	case <-b.stopCh:
		return
	default:
		close(b.stopCh)
	}

	select {
	case <-b.loopDone:
	default:
	}

	b.DrainWithTimeout(b.flushTimeout)
}

func (b *MeteringBuffer) GetBufferStats() BufferStats {
	b.mu.RLock()
	size := len(b.events)
	b.mu.RUnlock()
	return BufferStats{
		Size:           size,
		Capacity:       b.maxSize,
		EventsReplayed: b.eventsReplayed.Load(),
		EventsEvicted:  b.eventsEvicted.Load(),
	}
}

func (b *MeteringBuffer) replayLoop() {
	defer close(b.loopDone)
	defer func() {
		if r := recover(); r != nil {
			core.Error("[BUFFER] replayLoop panic: %v", r)
		}
	}()

	ticker := time.NewTicker(b.flushInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			b.flush()
		case <-b.stopCh:
			return
		}
	}
}

func (b *MeteringBuffer) flush() {
	b.flushMu.Lock()
	defer b.flushMu.Unlock()

	pending := b.takePending()
	if len(pending) == 0 {
		return
	}

	core.Debug("[BUFFER] flushing %d buffered events", len(pending))

	var remaining []BufferedEvent
	for i, event := range pending {
		err := replayEvent(event)
		if err == nil {
			b.eventsReplayed.Add(1)
			continue
		}
		classification := resilience.ClassifyError(err)
		if classification == resilience.ClassificationNonRetryable {
			core.Debug("[BUFFER] discarding event with non-retryable error: %v", err)
			b.eventsEvicted.Add(1)
			continue
		}
		remaining = append(remaining, pending[i:]...)
		break
	}

	if len(remaining) > 0 {
		b.putBack(remaining)
		core.Debug("[BUFFER] %d events remaining after flush", len(remaining))
	}
}

func (b *MeteringBuffer) takePending() []BufferedEvent {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.evictExpired()
	if len(b.events) == 0 {
		return nil
	}
	pending := make([]BufferedEvent, len(b.events))
	copy(pending, b.events)
	b.events = b.events[:0]
	return pending
}

func (b *MeteringBuffer) putBack(events []BufferedEvent) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.events = append(events, b.events...)
	if len(b.events) > b.maxSize {
		overflow := len(b.events) - b.maxSize
		b.events = b.events[overflow:]
		b.eventsEvicted.Add(int64(overflow))
	}
}

func (b *MeteringBuffer) evictExpired() {
	cutoff := time.Now().Add(-maxEventAge)
	idx := 0
	for _, e := range b.events {
		if e.CreatedAt.After(cutoff) {
			b.events[idx] = e
			idx++
		} else {
			b.eventsEvicted.Add(1)
		}
	}
	if evicted := len(b.events) - idx; evicted > 0 {
		core.Debug("[BUFFER] evicted %d expired events", evicted)
	}
	for i := idx; i < len(b.events); i++ {
		b.events[i] = BufferedEvent{}
	}
	b.events = b.events[:idx]
}

func replayEvent(event BufferedEvent) error {
	ctx, cancel := context.WithTimeout(context.Background(), replayRequestTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "POST", event.URL, bytes.NewReader(event.Body))
	if err != nil {
		return core.NewMeteringError("failed to create replay request", err)
	}

	for k, v := range event.Headers {
		req.Header.Set(k, v)
	}

	resp, err := sharedHTTPClient.Do(req)
	if err != nil {
		revErr := core.NewNetworkError("replay request failed", err)
		revErr.StatusCode = 503
		return revErr
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, io.LimitReader(resp.Body, maxReplayResponseBody))

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}

	classification := resilience.ClassifyHTTPResponse(resp.StatusCode, "")
	switch classification {
	case resilience.ClassificationRetryable, resilience.ClassificationThrottled:
		revErr := core.NewMeteringError("replay failed", nil)
		revErr.StatusCode = resp.StatusCode
		return revErr
	default:
		revErr := core.NewValidationError("replay rejected", nil)
		revErr.StatusCode = resp.StatusCode
		return revErr
	}
}
