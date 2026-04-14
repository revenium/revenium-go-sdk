package resilience

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewCircuitBreaker_Defaults(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{})

	assert.Equal(t, defaultFailureThreshold, cb.config.FailureThreshold)
	assert.Equal(t, defaultRecoveryTimeout, cb.config.RecoveryTimeout)
	assert.Equal(t, defaultSuccessThreshold, cb.config.SuccessThreshold)
	assert.Equal(t, defaultTimeWindow, cb.config.TimeWindow)
	assert.Equal(t, StateClosed, cb.state)
}

func TestCircuitBreaker_ClosedPassesThrough(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{})
	executed := false

	err := cb.Execute(func() error {
		executed = true
		return nil
	})

	assert.NoError(t, err)
	assert.True(t, executed)
	assert.Equal(t, StateClosed, cb.GetState())
}

func TestCircuitBreaker_OpensAfterThreshold(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		FailureThreshold: 3,
		TimeWindow:       time.Minute,
	})

	testErr := errors.New("fail")
	for i := 0; i < 3; i++ {
		_ = cb.Execute(func() error { return testErr })
	}

	assert.Equal(t, StateOpen, cb.GetState())
}

func TestCircuitBreaker_RejectsWhenOpen(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		FailureThreshold: 1,
		RecoveryTimeout:  time.Hour,
	})

	_ = cb.Execute(func() error { return errors.New("fail") })
	require.Equal(t, StateOpen, cb.GetState())

	executed := false
	err := cb.Execute(func() error {
		executed = true
		return nil
	})

	assert.ErrorIs(t, err, ErrCircuitOpen)
	assert.False(t, executed)
}

func TestCircuitBreaker_TransitionsToHalfOpen(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		FailureThreshold: 1,
		RecoveryTimeout:  10 * time.Millisecond,
	})

	_ = cb.Execute(func() error { return errors.New("fail") })
	require.Equal(t, StateOpen, cb.GetState())

	time.Sleep(15 * time.Millisecond)

	executed := false
	_ = cb.Execute(func() error {
		executed = true
		return nil
	})

	assert.True(t, executed)
	assert.Equal(t, StateHalfOpen, cb.GetState())
}

func TestCircuitBreaker_ClosesAfterSuccessThreshold(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		FailureThreshold: 1,
		RecoveryTimeout:  10 * time.Millisecond,
		SuccessThreshold: 2,
	})

	_ = cb.Execute(func() error { return errors.New("fail") })
	require.Equal(t, StateOpen, cb.GetState())

	time.Sleep(15 * time.Millisecond)

	_ = cb.Execute(func() error { return nil })
	assert.Equal(t, StateHalfOpen, cb.GetState())

	_ = cb.Execute(func() error { return nil })
	assert.Equal(t, StateClosed, cb.GetState())
}

func TestCircuitBreaker_ReopensOnHalfOpenFailure(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		FailureThreshold: 1,
		RecoveryTimeout:  10 * time.Millisecond,
		SuccessThreshold: 3,
	})

	_ = cb.Execute(func() error { return errors.New("fail") })
	require.Equal(t, StateOpen, cb.GetState())

	time.Sleep(15 * time.Millisecond)

	_ = cb.Execute(func() error { return nil })
	assert.Equal(t, StateHalfOpen, cb.GetState())

	_ = cb.Execute(func() error { return errors.New("fail again") })
	assert.Equal(t, StateOpen, cb.GetState())
}

func TestCircuitBreaker_TimeWindowExpiry(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		FailureThreshold: 3,
		TimeWindow:       50 * time.Millisecond,
	})

	_ = cb.Execute(func() error { return errors.New("fail") })
	_ = cb.Execute(func() error { return errors.New("fail") })

	time.Sleep(60 * time.Millisecond)

	_ = cb.Execute(func() error { return errors.New("fail") })

	assert.Equal(t, StateClosed, cb.GetState())
}

func TestCircuitBreaker_Reset(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{FailureThreshold: 1})

	_ = cb.Execute(func() error { return errors.New("fail") })
	require.Equal(t, StateOpen, cb.GetState())

	cb.Reset()

	assert.Equal(t, StateClosed, cb.GetState())
	stats := cb.GetStats()
	assert.Equal(t, 0, stats.SuccessCount)
	assert.Equal(t, 0, stats.RecentFailures)
}

func TestCircuitBreaker_GetStats(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		FailureThreshold: 5,
		RecoveryTimeout:  time.Hour,
		TimeWindow:       time.Minute,
	})

	stats := cb.GetStats()
	assert.Equal(t, StateClosed, stats.State)
	assert.Equal(t, 0, stats.RecentFailures)

	for i := 0; i < 5; i++ {
		_ = cb.Execute(func() error { return errors.New("fail") })
	}

	stats = cb.GetStats()
	assert.Equal(t, StateOpen, stats.State)
	assert.Equal(t, 5, stats.RecentFailures)
	assert.True(t, stats.TimeUntilRecovery > 0)
}

func TestCircuitBreaker_ConcurrentAccess(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		FailureThreshold: 100,
		TimeWindow:       time.Minute,
	})

	var wg sync.WaitGroup
	var execCount atomic.Int64
	goroutines := 50
	iterations := 20

	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		go func(id int) {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				_ = cb.Execute(func() error {
					execCount.Add(1)
					if id%2 == 0 {
						return errors.New("fail")
					}
					return nil
				})
				_ = cb.CanExecute()
				_ = cb.GetState()
				_ = cb.GetStats()
			}
		}(g)
	}

	wg.Wait()
	assert.True(t, execCount.Load() > 0)
}

func TestCircuitBreaker_ShouldTripFiltersErrors(t *testing.T) {
	strategy := &DefaultFailureStrategy{}
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		FailureThreshold: 2,
		ShouldTrip:       strategy.IsRetryableError,
	})

	for i := 0; i < 10; i++ {
		_ = cb.Execute(func() error {
			return core.NewValidationError("bad request", nil)
		})
	}
	assert.Equal(t, StateClosed, cb.GetState())

	_ = cb.Execute(func() error { return core.NewNetworkError("timeout", nil) })
	_ = cb.Execute(func() error { return core.NewNetworkError("timeout", nil) })
	assert.Equal(t, StateOpen, cb.GetState())
}

func TestCircuitBreaker_CanExecute(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		FailureThreshold: 1,
		RecoveryTimeout:  10 * time.Millisecond,
	})

	assert.True(t, cb.CanExecute())

	_ = cb.Execute(func() error { return errors.New("fail") })
	assert.False(t, cb.CanExecute())

	time.Sleep(15 * time.Millisecond)
	assert.True(t, cb.CanExecute())
}
