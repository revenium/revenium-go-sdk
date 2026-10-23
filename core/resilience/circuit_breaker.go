package resilience

import (
	"sync"
	"time"

	"github.com/revenium/revenium-go-sdk/core"
)

type State int

const (
	StateClosed State = iota
	StateOpen
	StateHalfOpen
)

func (s State) String() string {
	switch s {
	case StateClosed:
		return "CLOSED"
	case StateOpen:
		return "OPEN"
	case StateHalfOpen:
		return "HALF_OPEN"
	default:
		return "UNKNOWN"
	}
}

const (
	defaultFailureThreshold = 5
	defaultRecoveryTimeout  = 30 * time.Second
	defaultSuccessThreshold = 3
	defaultTimeWindow       = 60 * time.Second
	maxFailureHistory       = 1000
	periodicCleanupInterval = 5 * time.Minute
	periodicCleanupSize     = 100
)

type CircuitBreakerConfig struct {
	FailureThreshold int
	RecoveryTimeout  time.Duration
	SuccessThreshold int
	TimeWindow       time.Duration
	ShouldTrip       func(error) bool
	// ConsecutiveFailures makes a success forget earlier failures, so only
	// an unbroken run of FailureThreshold failures opens the breaker.
	ConsecutiveFailures bool
}

type CircuitBreakerStats struct {
	State             State
	SuccessCount      int
	RecentFailures    int
	TimeUntilRecovery time.Duration
}

type CircuitBreaker struct {
	state            State
	successCount     int
	reopenAt         time.Time
	lastCleanupTime  time.Time
	failures         []time.Time
	halfOpenInFlight bool
	generation       uint64
	config           CircuitBreakerConfig
	mu               sync.RWMutex
}

func NewCircuitBreaker(config CircuitBreakerConfig) *CircuitBreaker {
	if config.FailureThreshold <= 0 {
		config.FailureThreshold = defaultFailureThreshold
	}
	if config.RecoveryTimeout <= 0 {
		config.RecoveryTimeout = defaultRecoveryTimeout
	}
	if config.SuccessThreshold <= 0 {
		config.SuccessThreshold = defaultSuccessThreshold
	}
	if config.TimeWindow <= 0 {
		config.TimeWindow = defaultTimeWindow
	}
	return &CircuitBreaker{
		state:           StateClosed,
		failures:        make([]time.Time, 0),
		config:          config,
		lastCleanupTime: time.Now(),
	}
}

// Execute runs fn unless the breaker is open and records the outcome. Each
// call belongs to the breaker state it was admitted under: when the breaker
// has changed state since, the outcome is ignored, so a slow call admitted
// before the breaker opened cannot end a later half-open probe or reopen the
// breaker after the fact.
func (cb *CircuitBreaker) Execute(fn func() error) error {
	ticket, err := cb.admit()
	if err != nil {
		return err
	}

	defer func() {
		if r := recover(); r != nil {
			cb.settle(ticket, true)
			panic(r)
		}
	}()

	err = fn()
	if err == nil {
		cb.settle(ticket, false)
		return nil
	}
	if cb.config.ShouldTrip == nil || cb.config.ShouldTrip(err) {
		cb.settle(ticket, true)
	} else {
		cb.releaseProbe(ticket)
	}
	return err
}

func (cb *CircuitBreaker) admit() (uint64, error) {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	if cb.state == StateOpen {
		if time.Now().Before(cb.reopenAt) {
			return 0, ErrCircuitOpen
		}
		cb.moveTo(StateHalfOpen)
		cb.successCount = 0
		core.Info("[CIRCUIT_BREAKER] OPEN -> HALF_OPEN: attempting recovery")
	}
	if cb.state == StateHalfOpen {
		if cb.halfOpenInFlight {
			return 0, ErrCircuitOpen
		}
		cb.halfOpenInFlight = true
	}
	return cb.generation, nil
}

func (cb *CircuitBreaker) settle(ticket uint64, failed bool) {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	if ticket != cb.generation {
		return
	}
	if failed {
		cb.onFailure()
	} else {
		cb.onSuccess()
	}
}

func (cb *CircuitBreaker) releaseProbe(ticket uint64) {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	if ticket == cb.generation {
		cb.halfOpenInFlight = false
	}
}

func (cb *CircuitBreaker) moveTo(state State) {
	cb.state = state
	cb.generation++
}

func (cb *CircuitBreaker) CanExecute() bool {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	if cb.state == StateClosed {
		return true
	}
	if cb.state == StateHalfOpen {
		return !cb.halfOpenInFlight
	}
	return !time.Now().Before(cb.reopenAt)
}

func (cb *CircuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.onSuccess()
}

func (cb *CircuitBreaker) RecordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.onFailure()
}

func (cb *CircuitBreaker) GetState() State {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.state
}

func (cb *CircuitBreaker) GetStats() CircuitBreakerStats {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	stats := CircuitBreakerStats{
		State:          cb.state,
		SuccessCount:   cb.successCount,
		RecentFailures: len(cb.failures),
	}

	if cb.state == StateOpen {
		if wait := time.Until(cb.reopenAt); wait > 0 {
			stats.TimeUntilRecovery = wait
		}
	}

	return stats
}

func (cb *CircuitBreaker) Reset() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.moveTo(StateClosed)
	cb.successCount = 0
	cb.reopenAt = time.Time{}
	cb.failures = make([]time.Time, 0)
	cb.halfOpenInFlight = false
	cb.lastCleanupTime = time.Now()
}

func (cb *CircuitBreaker) onSuccess() {
	switch cb.state {
	case StateHalfOpen:
		cb.halfOpenInFlight = false
		cb.successCount++
		if cb.successCount >= cb.config.SuccessThreshold {
			cb.moveTo(StateClosed)
			cb.successCount = 0
			cb.failures = make([]time.Time, 0)
			core.Info("[CIRCUIT_BREAKER] HALF_OPEN -> CLOSED: recovery successful")
		}
	case StateClosed:
		if cb.config.ConsecutiveFailures {
			cb.failures = cb.failures[:0]
		}
		cb.performPeriodicCleanup()
	}
}

func (cb *CircuitBreaker) onFailure() {
	now := time.Now()
	cb.failures = append(cb.failures, now)

	if len(cb.failures) > maxFailureHistory {
		half := len(cb.failures) / 2
		cb.failures = cb.failures[half:]
	}

	cb.cleanupOldFailures()

	switch cb.state {
	case StateHalfOpen:
		cb.halfOpenInFlight = false
		cb.moveTo(StateOpen)
		cb.successCount = 0
		cb.reopenAt = now.Add(cb.config.RecoveryTimeout)
		core.Warn("[CIRCUIT_BREAKER] HALF_OPEN -> OPEN: failure during recovery")
	case StateClosed:
		if len(cb.failures) >= cb.config.FailureThreshold {
			cb.moveTo(StateOpen)
			cb.successCount = 0
			cb.reopenAt = now.Add(cb.config.RecoveryTimeout)
			core.Warn("[CIRCUIT_BREAKER] CLOSED -> OPEN: failure threshold reached (%d/%d)", len(cb.failures), cb.config.FailureThreshold)
		}
	}
}

// HoldOpenFor keeps the breaker open for at least d, for a server that said
// how long to wait (Retry-After) before it is called again.
func (cb *CircuitBreaker) HoldOpenFor(d time.Duration) {
	if d <= 0 {
		return
	}
	cb.mu.Lock()
	defer cb.mu.Unlock()

	if until := time.Now().Add(d); until.After(cb.reopenAt) {
		cb.reopenAt = until
	}
	if cb.state == StateOpen {
		return
	}
	core.Warn("[CIRCUIT_BREAKER] %s -> OPEN: the server asked to wait %s before the next request", cb.state, d)
	cb.moveTo(StateOpen)
	cb.successCount = 0
	cb.halfOpenInFlight = false
}

func (cb *CircuitBreaker) cleanupOldFailures() {
	cutoff := time.Now().Add(-cb.config.TimeWindow)
	idx := 0
	for _, t := range cb.failures {
		if t.After(cutoff) {
			cb.failures[idx] = t
			idx++
		}
	}
	cb.failures = cb.failures[:idx]
}

func (cb *CircuitBreaker) performPeriodicCleanup() {
	now := time.Now()
	if now.Sub(cb.lastCleanupTime) >= periodicCleanupInterval || len(cb.failures) > periodicCleanupSize {
		cb.cleanupOldFailures()
		cb.lastCleanupTime = now
	}
}
