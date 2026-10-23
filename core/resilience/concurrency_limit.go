package resilience

import "sync/atomic"

// ConcurrencyLimit caps how many calls run at once; a nil limit does not cap.
type ConcurrencyLimit struct {
	slots   chan struct{}
	waiting atomic.Int64
}

func NewConcurrencyLimit(size int) *ConcurrencyLimit {
	return &ConcurrencyLimit{slots: make(chan struct{}, size)}
}

// Run waits for a free place, then runs fn.
func (l *ConcurrencyLimit) Run(fn func() error) error {
	if l == nil {
		return fn()
	}
	select {
	case l.slots <- struct{}{}:
	default:
		l.waiting.Add(1)
		l.slots <- struct{}{}
		l.waiting.Add(-1)
	}
	defer func() { <-l.slots }()
	return fn()
}

// Waiting reports how many calls are queued for a free place.
func (l *ConcurrencyLimit) Waiting() int {
	if l == nil {
		return 0
	}
	return int(l.waiting.Load())
}
