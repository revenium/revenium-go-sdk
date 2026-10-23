package core

import (
	"context"
	"sync"
	"time"
)

// AbandonedWaitLimit is how long a provider module keeps polling, in the background, a job the
// caller stopped waiting for. Media providers finish a job in minutes, so one still unknown after
// this is recorded as unfinished, with the requested quantities, rather than held for a provider's
// 30-minute limit: a process that exits without calling Close() loses whatever is still being polled.
const AbandonedWaitLimit = 10 * time.Minute

// BackgroundWaits runs work that outlives the call that started it, such as polling a provider
// job the caller stopped waiting for, and ends it when the owning client closes. The zero value
// is ready to use.
type BackgroundWaits struct {
	mu      sync.Mutex
	ctx     context.Context
	cancel  context.CancelFunc
	stopped bool
	running sync.WaitGroup
}

// Start runs wait in its own goroutine with a context that Stop cancels. After Stop, wait runs
// synchronously with that already-cancelled context, so it can still settle what it was given.
func (w *BackgroundWaits) Start(wait func(stop context.Context)) {
	w.mu.Lock()
	w.ensureContext()
	stop := w.ctx
	if w.stopped {
		w.mu.Unlock()
		wait(stop)
		return
	}
	w.running.Add(1)
	w.mu.Unlock()

	go func() {
		defer w.running.Done()
		wait(stop)
	}()
}

// Stop cancels every running wait and returns once they have all returned, so whatever they
// send on the way out is sent before the caller shuts down its own clients.
func (w *BackgroundWaits) Stop() {
	w.mu.Lock()
	w.ensureContext()
	w.stopped = true
	w.cancel()
	w.mu.Unlock()
	w.running.Wait()
}

func (w *BackgroundWaits) ensureContext() {
	if w.ctx == nil {
		w.ctx, w.cancel = context.WithCancel(context.Background())
	}
}
