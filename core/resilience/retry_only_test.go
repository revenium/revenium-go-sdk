package resilience

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func onlyThrottled(err error) bool {
	return ClassifyError(err) == ClassificationThrottled
}

func statusError(status int) *core.ReveniumError {
	err := core.NewMeteringError("revenium answered", nil)
	err.StatusCode = status
	return err
}

func TestWithRetry_RetryOnlyStopsAtAFailureItDoesNotAccept(t *testing.T) {
	var calls atomic.Int32
	config := &RetryConfig{MaxRetries: 3, BaseDelay: time.Millisecond, MaxDelay: 10 * time.Millisecond, RetryOnly: onlyThrottled}

	err := WithRetry(context.Background(), func() error {
		calls.Add(1)
		return statusError(502)
	}, config)

	var revErr *core.ReveniumError
	require.ErrorAs(t, err, &revErr)
	assert.Equal(t, 502, revErr.StatusCode)
	assert.NotContains(t, err.Error(), "failed after")
	assert.Equal(t, int32(1), calls.Load())
}

func TestWithRetry_RetryOnlyStillRetriesTheFailuresItAccepts(t *testing.T) {
	var calls atomic.Int32
	config := &RetryConfig{MaxRetries: 3, BaseDelay: time.Millisecond, MaxDelay: 10 * time.Millisecond, RetryOnly: onlyThrottled}

	err := WithRetry(context.Background(), func() error {
		if calls.Add(1) < 3 {
			return statusError(429)
		}
		return nil
	}, config)

	require.NoError(t, err)
	assert.Equal(t, int32(3), calls.Load())
}

func TestWithRetry_RetryOnlyCannotRetryAFailureTheClassifierRefuses(t *testing.T) {
	var calls atomic.Int32
	config := &RetryConfig{MaxRetries: 3, BaseDelay: time.Millisecond, MaxDelay: 10 * time.Millisecond, RetryOnly: func(error) bool { return true }}

	err := WithRetry(context.Background(), func() error {
		calls.Add(1)
		return statusError(400)
	}, config)

	require.Error(t, err)
	assert.Equal(t, int32(1), calls.Load())
}

func TestWithRetry_RetryAlsoRetriesAFailureTheClassifierRefusesAndKeepsItsChain(t *testing.T) {
	var calls atomic.Int32
	timeout := core.NewNetworkError("request failed", context.DeadlineExceeded)
	var allowed atomic.Int32
	config := &RetryConfig{MaxRetries: 3, BaseDelay: time.Millisecond, MaxDelay: 10 * time.Millisecond,
		RetryAlso: func(err error) bool { return errors.Is(err, context.DeadlineExceeded) && allowed.Add(1) == 1 }}

	err := WithRetry(context.Background(), func() error {
		calls.Add(1)
		return timeout
	}, config)

	assert.Same(t, timeout, err)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Equal(t, int32(2), calls.Load())
}

func TestRetryThroughBreaker_RetryAlsoNeverRetriesPastAnOpenBreaker(t *testing.T) {
	breaker := NewCircuitBreaker(CircuitBreakerConfig{FailureThreshold: 1, RecoveryTimeout: time.Minute, ConsecutiveFailures: true})
	var calls, asked atomic.Int32
	config := &RetryConfig{MaxRetries: 3, BaseDelay: time.Millisecond, MaxDelay: 10 * time.Millisecond,
		RetryAlso: func(error) bool { asked.Add(1); return true }}

	err := RetryThroughBreaker(breaker, nil, config, func() error {
		calls.Add(1)
		return statusError(503)
	})

	assert.ErrorIs(t, err, ErrCircuitOpen)
	assert.NotContains(t, err.Error(), "failed after", "the call stops at the open breaker instead of using up its retries")
	assert.Equal(t, int32(0), asked.Load(), "RetryAlso is never asked to override an open breaker")
	assert.Equal(t, int32(1), calls.Load())
}

func TestWithRetry_RetryAlsoNeverOverridesALongRetryAfter(t *testing.T) {
	var calls, asked atomic.Int32
	config := &RetryConfig{MaxRetries: 3, BaseDelay: time.Millisecond, MaxDelay: 10 * time.Millisecond,
		RetryAlso: func(error) bool { asked.Add(1); return true }}

	err := WithRetry(context.Background(), func() error {
		calls.Add(1)
		throttled := statusError(503)
		throttled.RetryAfter = time.Minute
		return throttled
	}, config)

	require.Error(t, err)
	assert.Equal(t, int32(1), calls.Load(), "the server asked to wait longer than the retry budget")
	assert.Equal(t, int32(0), asked.Load())
}

func TestWithRetry_RetryOnlyIsAHardFilterEvenWhenRetryAlsoAccepts(t *testing.T) {
	var calls, asked atomic.Int32
	config := &RetryConfig{MaxRetries: 3, BaseDelay: time.Millisecond, MaxDelay: 10 * time.Millisecond,
		RetryOnly: onlyThrottled,
		RetryAlso: func(error) bool { asked.Add(1); return true }}

	err := WithRetry(context.Background(), func() error {
		calls.Add(1)
		return core.NewNetworkError("request failed", context.DeadlineExceeded)
	}, config)

	require.Error(t, err)
	assert.Equal(t, int32(1), calls.Load(), "a write that must not be repeated is not resent after a timeout")
	assert.Equal(t, int32(0), asked.Load(), "RetryAlso is not asked, so a once-only allowance is not used up")
}

func TestWithRetry_RetryAlsoIsNotAskedForFailuresTheClassifierRetries(t *testing.T) {
	asked := 0
	config := &RetryConfig{
		MaxRetries: 1,
		BaseDelay:  time.Millisecond,
		MaxDelay:   time.Millisecond,
		RetryAlso: func(error) bool {
			asked++
			return true
		},
	}
	assert.True(t, config.retries(core.NewNetworkError("connection reset", nil)))
	assert.Zero(t, asked)
}
