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
}

type CircuitBreakerStats struct {
	State             State
	SuccessCount      int
	RecentFailures    int
	TimeUntilRecovery time.Duration
}

type CircuitBreaker struct {
	state             State
	successCount      int
	lastFailureTime   time.Time
	lastCleanupTime   time.Time
	failures          []time.Time
	halfOpenInFlight  bool
	config            CircuitBreakerConfig
	mu                sync.RWMutex
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

func (cb *CircuitBreaker) Execute(fn func() error) error {
	cb.mu.Lock()
	if cb.state == StateOpen {
		if time.Since(cb.lastFailureTime) >= cb.config.RecoveryTimeout {
			cb.state = StateHalfOpen
			cb.successCount = 0
			core.Info("[CIRCUIT_BREAKER] OPEN -> HALF_OPEN: attempting recovery")
		} else {
			cb.mu.Unlock()
			return ErrCircuitOpen
		}
	}
	if cb.state == StateHalfOpen && cb.halfOpenInFlight {
		cb.mu.Unlock()
		return ErrCircuitOpen
	}
	if cb.state == StateHalfOpen {
		cb.halfOpenInFlight = true
	}
	cb.mu.Unlock()

	defer func() {
		if r := recover(); r != nil {
			cb.RecordFailure()
			panic(r)
		}
	}()

	err := fn()

	if err != nil {
		if cb.config.ShouldTrip == nil || cb.config.ShouldTrip(err) {
			cb.RecordFailure()
		} else {
			cb.clearHalfOpenInFlight()
		}
	} else {
		cb.RecordSuccess()
	}

	return err
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
	return time.Since(cb.lastFailureTime) >= cb.config.RecoveryTimeout
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
		elapsed := time.Since(cb.lastFailureTime)
		if elapsed < cb.config.RecoveryTimeout {
			stats.TimeUntilRecovery = cb.config.RecoveryTimeout - elapsed
		}
	}

	return stats
}

func (cb *CircuitBreaker) Reset() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.state = StateClosed
	cb.successCount = 0
	cb.lastFailureTime = time.Time{}
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
			cb.state = StateClosed
			cb.successCount = 0
			cb.failures = make([]time.Time, 0)
			core.Info("[CIRCUIT_BREAKER] HALF_OPEN -> CLOSED: recovery successful")
		}
	case StateClosed:
		cb.performPeriodicCleanup()
	}
}

func (cb *CircuitBreaker) onFailure() {
	now := time.Now()
	cb.lastFailureTime = now
	cb.failures = append(cb.failures, now)

	if len(cb.failures) > maxFailureHistory {
		half := len(cb.failures) / 2
		cb.failures = cb.failures[half:]
	}

	cb.cleanupOldFailures()

	switch cb.state {
	case StateHalfOpen:
		cb.halfOpenInFlight = false
		cb.state = StateOpen
		cb.successCount = 0
		core.Warn("[CIRCUIT_BREAKER] HALF_OPEN -> OPEN: failure during recovery")
	case StateClosed:
		if len(cb.failures) >= cb.config.FailureThreshold {
			cb.state = StateOpen
			cb.successCount = 0
			core.Warn("[CIRCUIT_BREAKER] CLOSED -> OPEN: failure threshold reached (%d/%d)", len(cb.failures), cb.config.FailureThreshold)
		}
	}
}

func (cb *CircuitBreaker) clearHalfOpenInFlight() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
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
