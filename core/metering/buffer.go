package metering

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/revenium/revenium-go-sdk/core/resilience"
)

// The buffer limits match the Python SDK's store-and-forward buffer: room for
// about eleven minutes of records at 30 calls a second, and up to 16 resends
// in flight while a backlog drains. A flush sends one record first, so an
// endpoint that is still down costs one request rather than sixteen.
const (
	defaultBufferMaxSize       = 20000
	replayConcurrency          = 16
	defaultBufferFlushInterval = 30 * time.Second
	defaultBufferFlushTimeout  = 10 * time.Second
	maxEventAge                = 24 * time.Hour
)

type BufferedEvent struct {
	URL            string
	Headers        map[string]string
	Body           []byte
	IdempotencyKey string
	CreatedAt      time.Time
}

// heldBytes is what one held record costs in memory: its serialised body
// plus the URL and headers it is resent with.
func (e BufferedEvent) heldBytes() int64 {
	size := len(e.Body) + len(e.URL) + len(e.IdempotencyKey)
	for name, value := range e.Headers {
		size += len(name) + len(value)
	}
	return int64(size)
}

func totalHeldBytes(events []BufferedEvent) int64 {
	var total int64
	for _, e := range events {
		total += e.heldBytes()
	}
	return total
}

// BufferStats is a snapshot of a client's retry buffer. Size counts the
// records queued for the next flush and InFlight the records a flush is
// resending now; HeldBytes is the memory those take together with the
// client's records waiting for or in their first send, the amount MaxBytes
// bounds.
type BufferStats struct {
	Size           int
	InFlight       int
	Capacity       int
	HeldBytes      int64
	MaxBytes       int64
	EventsReplayed int64
	EventsEvicted  int64
}

type bufferLimits struct {
	records int
	bytes   int64
}

func (l bufferLimits) withDefaults() bufferLimits {
	if l.records <= 0 {
		l.records = defaultBufferMaxSize
	}
	if l.bytes <= 0 {
		l.bytes = core.BufferMaxBytes()
	}
	return l
}

type MeteringBuffer struct {
	events []BufferedEvent
	limits bufferLimits
	// queuedBytes == totalHeldBytes(events).
	queuedBytes int64
	// inFlightBytes is the bytes of the records takePending handed to a flush
	// that are not yet settled or put back.
	inFlightBytes int64
	// sendingBytes is the bytes of the client's records waiting in the
	// delivery queue or in their first send, reserved by reserveForSend.
	sendingBytes   int64
	flushInterval  time.Duration
	flushTimeout   time.Duration
	mu             sync.RWMutex
	flushMu        sync.Mutex
	stopCh         chan struct{}
	loopDone       chan struct{}
	startOnce      sync.Once
	eventsReplayed atomic.Int64
	eventsEvicted  atomic.Int64
	inFlight       int
	full           atomic.Bool
	resendFull     atomic.Bool
	path           *deliveryPath
	replayPool     *deliveryPool
	wake           chan struct{}
}

func NewMeteringBuffer(maxSize int, flushInterval, flushTimeout time.Duration) *MeteringBuffer {
	path := newDeliveryPath(resilience.NewCircuitBreaker(resilience.DeliveryCircuitBreakerConfig()), defaultSender)
	buffer := newMeteringBuffer(bufferLimits{records: maxSize}, flushInterval, flushTimeout, path)
	path.monitor.onRecovery = buffer.requestReplay
	return buffer
}

func newMeteringBuffer(limits bufferLimits, flushInterval, flushTimeout time.Duration, path *deliveryPath) *MeteringBuffer {
	if flushInterval <= 0 {
		flushInterval = defaultBufferFlushInterval
	}
	if flushTimeout <= 0 {
		flushTimeout = defaultBufferFlushTimeout
	}
	return &MeteringBuffer{
		events:        make([]BufferedEvent, 0, 64),
		limits:        limits.withDefaults(),
		flushInterval: flushInterval,
		flushTimeout:  flushTimeout,
		stopCh:        make(chan struct{}),
		loopDone:      make(chan struct{}),
		path:          path,
		replayPool:    sharedReplayPool,
		wake:          make(chan struct{}, 1),
	}
}

func (b *MeteringBuffer) Push(event BufferedEvent) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.push(event)
}

// push holds a record for the next flush. Callers hold b.mu.
func (b *MeteringBuffer) push(event BufferedEvent) {
	size := event.heldBytes()
	if !b.fitsBesideUndroppable(size) {
		b.dropNewest(size)
		return
	}
	b.evictOldestUntil(b.limits.records-1, b.limits.bytes-size)
	event.CreatedAt = time.Now()
	b.events = append(b.events, event)
	b.queuedBytes += size
	core.Debug("[BUFFER] event buffered, depth: %d/%d, %d/%d bytes", len(b.events), b.limits.records, b.heldBytes(), b.limits.bytes)

	b.startOnce.Do(func() {
		go b.replayLoop()
	})
}

// fitsBesideUndroppable reports whether a record of size bytes fits the byte
// limit once every queued record is dropped. Records a flush is resending and
// records waiting for or in their first send cannot be dropped, so a record
// that does not fit beside them is the one dropped.
func (b *MeteringBuffer) fitsBesideUndroppable(size int64) bool {
	return b.inFlightBytes+b.sendingBytes+size <= b.limits.bytes
}

// heldBytes is everything the byte limit counts. Callers hold b.mu.
func (b *MeteringBuffer) heldBytes() int64 {
	return b.queuedBytes + b.inFlightBytes + b.sendingBytes
}

// reserveForSend counts a record about to enter the delivery queue against
// the byte limit, dropping the oldest held records to make room. It returns
// nil, with the once-per-outage warning, when the records already being sent
// or resent leave no room, and the caller drops the record.
func (b *MeteringBuffer) reserveForSend(size int64) *sendReservation {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.fitsBesideUndroppable(size) {
		b.eventsEvicted.Add(1)
		if size > b.limits.bytes {
			core.Warn("[BUFFER] dropped a usage record of %d bytes, larger than %s (%d bytes)", size, core.EnvBufferMaxBytes, b.limits.bytes)
			return nil
		}
		b.warnFullOncePerOutage()
		return nil
	}
	b.evictOldestUntil(len(b.events), b.limits.bytes-size)
	b.sendingBytes += size
	return &sendReservation{buffer: b, size: size}
}

// sendReservation is the share of the byte limit a record holds from the
// moment it enters the delivery queue until its send is over. A send can
// panic, so the delivery job also releases it in a deferred call, and release
// returns the bytes only the first time it is called: the deferred call after
// a normal release does nothing. It is used by one delivery job at a time.
type sendReservation struct {
	buffer  *MeteringBuffer
	size    int64
	settled bool
}

func (r *sendReservation) release() {
	if r.settled {
		return
	}
	r.settled = true
	r.buffer.releaseSend(r.size)
}

// hold turns the reservation into a held record for the next flush, unless it
// was already returned.
func (r *sendReservation) hold(event BufferedEvent) {
	if r.settled {
		return
	}
	r.settled = true
	r.buffer.holdSent(event, r.size)
}

// holdSent moves a record whose send failed from the send reservations to the
// held records in one critical section. Released first and pushed after, its
// bytes would be free in between: a concurrent reservation could take them
// and the record would then be dropped as the one that does not fit, while
// both stayed in memory.
func (b *MeteringBuffer) holdSent(event BufferedEvent, size int64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.sendingBytes -= size
	b.push(event)
}

// releaseSend returns the reservation of a record that was delivered or
// dropped. With nothing held and the queue down to half the limit the outage
// is over.
func (b *MeteringBuffer) releaseSend(size int64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.sendingBytes -= size
	if len(b.events) == 0 && b.inFlight == 0 {
		b.endOutage()
	}
}

// dropNewest drops the record being pushed with a warning of its own. It does
// not latch the once-per-outage warning: the dropped record is never queued,
// so a latch set here might not be cleared by a flush (the replay loop may not
// be running yet) and would silence the warning for the next real outage. A
// record that does not fit beside the records a flush is resending, or beside
// the records waiting for or in their first send, warns through its own latch,
// because every record pushed during a long resend or a slow first send would
// otherwise log a line of its own. takePending re-arms that latch at the start
// of each resend (a resend that fails puts its records back and the next flush
// stalls again), and endOutage re-arms it with the outage warning.
func (b *MeteringBuffer) dropNewest(size int64) {
	b.eventsEvicted.Add(1)
	if size > b.limits.bytes {
		core.Warn("[BUFFER] dropped a usage record of %d bytes, larger than %s (%d bytes)", size, core.EnvBufferMaxBytes, b.limits.bytes)
		return
	}
	if b.resendFull.CompareAndSwap(false, true) {
		core.Warn("[BUFFER] dropping new usage records because the records being sent or resent fill %s (%d bytes)", core.EnvBufferMaxBytes, b.limits.bytes)
	}
}

// endOutage re-arms the warnings unless the records waiting for their first
// send still fill more than half the limit: a slow Revenium keeps the queue
// full without failing a request, and that is one outage, not one per flush.
// Callers hold b.mu.
func (b *MeteringBuffer) endOutage() {
	if b.sendingBytes > b.limits.bytes/2 {
		return
	}
	b.full.Store(false)
	b.resendFull.Store(false)
}

func (b *MeteringBuffer) DrainWithTimeout(timeout time.Duration) {
	b.drain(timeout)
}

func (b *MeteringBuffer) drain(timeout time.Duration) bool {
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer func() {
			if r := recover(); r != nil {
				core.Error("[BUFFER] panic while draining held usage records: %v", r)
			}
		}()
		b.flush()
	}()
	select {
	case <-done:
		return true
	case <-time.After(timeout):
		core.Warn("[BUFFER] drain timed out after %s", timeout)
		return false
	}
}

func (b *MeteringBuffer) Stop() {
	b.stop()
}

func (b *MeteringBuffer) stop() bool {
	select {
	case <-b.stopCh:
		return b.idleWithin(b.flushTimeout)
	default:
		close(b.stopCh)
	}
	return b.drain(b.flushTimeout)
}

func (b *MeteringBuffer) waitUntilIdle() {
	b.flushMu.Lock()
	defer b.flushMu.Unlock()
}

func (b *MeteringBuffer) idleWithin(timeout time.Duration) bool {
	idle := make(chan struct{})
	go func() {
		defer close(idle)
		defer func() {
			if r := recover(); r != nil {
				core.Error("[BUFFER] panic while waiting for a drain to finish: %v", r)
			}
		}()
		b.waitUntilIdle()
	}()
	select {
	case <-idle:
		return true
	case <-time.After(timeout):
		return false
	}
}

func (b *MeteringBuffer) GetBufferStats() BufferStats {
	b.mu.RLock()
	size := len(b.events)
	inFlight := b.inFlight
	held := b.heldBytes()
	b.mu.RUnlock()
	return BufferStats{
		Size:           size,
		InFlight:       inFlight,
		Capacity:       b.limits.records,
		HeldBytes:      held,
		MaxBytes:       b.limits.bytes,
		EventsReplayed: b.eventsReplayed.Load(),
		EventsEvicted:  b.eventsEvicted.Load(),
	}
}

// Undelivered counts the records held for replay, including any a drain that
// timed out is still trying to send.
func (b *MeteringBuffer) Undelivered() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.events) + b.inFlight
}

func (b *MeteringBuffer) requestReplay() {
	select {
	case b.wake <- struct{}{}:
	default:
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
		case <-b.wake:
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
	b.putBack(b.replay(pending))
}

// replay sends the pending records a window at a time: one record first, then
// up to replayConcurrency at once. It stops at the first window with a
// failure worth retrying and returns that window's retryable failures plus
// every record in the windows it did not send.
func (b *MeteringBuffer) replay(pending []BufferedEvent) []BufferedEvent {
	for start, window := 0, 1; start < len(pending); start, window = start+window, replayConcurrency {
		end := min(start+window, len(pending))
		batch := pending[start:end]
		kept := b.settle(batch, b.sendAll(batch))
		b.releaseSettled(batch, kept)
		if len(kept) > 0 {
			return append(kept, pending[end:]...)
		}
	}
	return nil
}

func (b *MeteringBuffer) sendAll(batch []BufferedEvent) []error {
	errs := make([]error, len(batch))
	var wg sync.WaitGroup
	for i := range batch {
		i := i
		wg.Add(1)
		b.replayPool.submit(func() {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					core.Error("[BUFFER] panic while resending a held usage record: %v", r)
					errs[i] = core.NewMeteringError(fmt.Sprintf("resending a held usage record panicked: %v", r), nil)
				}
			}()
			errs[i] = b.send(batch[i])
		})
	}
	wg.Wait()
	return errs
}

func (b *MeteringBuffer) send(event BufferedEvent) error {
	return b.path.sendOnce(func() error { return b.path.sender.post(event.URL, event.Body, event.Headers, "replay") })
}

func (b *MeteringBuffer) settle(batch []BufferedEvent, errs []error) []BufferedEvent {
	var kept []BufferedEvent
	for i, err := range errs {
		b.path.monitor.record(err)
		switch {
		case err == nil:
			b.eventsReplayed.Add(1)
		case isBufferable(err):
			kept = append(kept, batch[i])
		default:
			core.Warn("[BUFFER] dropping a held usage record that cannot be delivered: %v", err)
			b.eventsEvicted.Add(1)
		}
	}
	return kept
}

func (b *MeteringBuffer) takePending() []BufferedEvent {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.evictExpired()
	if len(b.events) == 0 {
		b.endOutage()
		return nil
	}
	pending := make([]BufferedEvent, len(b.events))
	copy(pending, b.events)
	b.events = make([]BufferedEvent, 0, 64)
	b.inFlight = len(pending)
	b.resendFull.Store(false)
	b.inFlightBytes = b.queuedBytes
	b.queuedBytes = 0
	return pending
}

// releaseSettled hands back the byte budget of the batch records that need no
// retry. It clears the batch entries first: a concurrent Push may spend the
// released budget at once, so a delivered body must already be unreachable.
// The retryable records stay reachable through kept for putBack.
func (b *MeteringBuffer) releaseSettled(batch, kept []BufferedEvent) {
	settledBytes := totalHeldBytes(batch) - totalHeldBytes(kept)
	clear(batch)
	b.markSettled(len(batch)-len(kept), settledBytes)
}

func (b *MeteringBuffer) markSettled(count int, bytes int64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.inFlight -= count
	b.inFlightBytes -= bytes
}

func (b *MeteringBuffer) putBack(events []BufferedEvent) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.inFlight -= len(events)
	if len(events) == 0 {
		b.endOutage()
		return
	}
	returned := totalHeldBytes(events)
	b.inFlightBytes -= returned
	b.queuedBytes += returned
	b.events = append(events, b.events...)
	b.evictOldestUntil(b.limits.records, b.limits.bytes)
	core.Debug("[BUFFER] %d events remaining after flush", len(b.events))
}

func (b *MeteringBuffer) evictExpired() {
	cutoff := time.Now().Add(-maxEventAge)
	idx := 0
	for _, e := range b.events {
		if e.CreatedAt.After(cutoff) {
			b.events[idx] = e
			idx++
		} else {
			b.queuedBytes -= e.heldBytes()
			b.eventsEvicted.Add(1)
		}
	}
	if evicted := len(b.events) - idx; evicted > 0 {
		core.Warn("[BUFFER] dropped %d usage records held for more than %s without reaching Revenium", evicted, maxEventAge)
	}
	for i := idx; i < len(b.events); i++ {
		b.events[i] = BufferedEvent{}
	}
	b.events = b.events[:idx]
}

// evictOldestUntil drops the oldest queued records until at most maxRecords
// are queued and the held bytes, counting records a flush is resending and
// records waiting for their first send, are at most maxBytes, or until nothing
// queued is left to drop.
func (b *MeteringBuffer) evictOldestUntil(maxRecords int, maxBytes int64) {
	dropped := 0
	for dropped < len(b.events) && (len(b.events)-dropped > maxRecords || b.heldBytes() > maxBytes) {
		b.queuedBytes -= b.events[dropped].heldBytes()
		b.events[dropped] = BufferedEvent{}
		dropped++
	}
	if dropped == 0 {
		return
	}
	b.warnFullOncePerOutage()
	b.events = b.events[dropped:]
	b.eventsEvicted.Add(int64(dropped))
}

// warnFullOncePerOutage warns the first time a record is dropped for room:
// a held record evicted to make room for a newer one, or a new record that
// reserveForSend refuses because the records being sent or resent fill the
// limit. endOutage is the one place that re-arms it. It runs when nothing is
// held or being resent, after a flush delivers every record it took, after
// the held records expire or when a send reservation is returned, and it
// leaves the warning latched while the records waiting for their first send
// fill more than half the limit.
func (b *MeteringBuffer) warnFullOncePerOutage() {
	if b.full.CompareAndSwap(false, true) {
		core.Warn("[BUFFER] buffer full (%d records or %d bytes, counting records waiting to be sent); dropping usage records, oldest held first, until Revenium accepts them again", b.limits.records, b.limits.bytes)
	}
}
