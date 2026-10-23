package resilience

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func throttledError(retryAfter time.Duration) error {
	err := core.NewNetworkError("metering API returned 429", nil)
	err.StatusCode = http.StatusTooManyRequests
	err.RetryAfter = retryAfter
	return err
}

func TestParseRetryAfter(t *testing.T) {
	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)

	assert.Equal(t, 7*time.Second, ParseRetryAfter("7", now))
	assert.Equal(t, 30*time.Second, ParseRetryAfter(now.Add(30*time.Second).Format(http.TimeFormat), now))
	assert.Equal(t, MaxRetryAfter, ParseRetryAfter("999999", now))
	assert.Zero(t, ParseRetryAfter("", now))
	assert.Zero(t, ParseRetryAfter("soon", now))
	assert.Zero(t, ParseRetryAfter("-3", now))
	assert.Zero(t, ParseRetryAfter(now.Add(-time.Minute).Format(http.TimeFormat), now))
}

func TestAcquireCircuitBreaker_IsSharedPerKeyAndSeparateAcrossKeys(t *testing.T) {
	ResetCircuitBreakers()
	t.Cleanup(ResetCircuitBreakers)

	a, releaseA := AcquireCircuitBreaker(MeteringEndpoint, "https://api.revenium.ai/", "hak_a")
	sameA, releaseSameA := AcquireCircuitBreaker(MeteringEndpoint, "https://api.revenium.ai", "hak_a")
	b, releaseB := AcquireCircuitBreaker(MeteringEndpoint, "https://api.revenium.ai", "hak_b")
	t.Cleanup(func() { releaseA(); releaseSameA(); releaseB() })

	assert.Same(t, a, sameA)
	assert.NotSame(t, a, b)
	for i := 0; i < deliveryFailuresToOpen; i++ {
		_ = a.Execute(func() error { return throttledError(0) })
	}
	assert.Equal(t, StateOpen, a.GetState())
	assert.Equal(t, StateClosed, b.GetState())
}

func TestAcquireCircuitBreaker_ForgetsAKeyOnceItsLastUserReleasesIt(t *testing.T) {
	ResetCircuitBreakers()
	t.Cleanup(ResetCircuitBreakers)

	first, releaseFirst := AcquireCircuitBreaker(MeteringEndpoint, "https://api.revenium.ai", "hak_rotated")
	second, releaseSecond := AcquireCircuitBreaker(MeteringEndpoint, "https://api.revenium.ai", "hak_rotated")
	for i := 0; i < deliveryFailuresToOpen; i++ {
		_ = first.Execute(func() error { return throttledError(0) })
	}

	releaseFirst()
	releaseFirst()
	assert.Equal(t, 1, scopedBreakerCount(), "a client still using the key keeps its breaker")
	assert.Equal(t, StateOpen, second.GetState())

	releaseSecond()
	assert.Equal(t, 0, scopedBreakerCount())
	fresh, releaseFresh := AcquireCircuitBreaker(MeteringEndpoint, "https://api.revenium.ai", "hak_rotated")
	t.Cleanup(releaseFresh)
	assert.Equal(t, StateClosed, fresh.GetState())
}

func TestDeliveryBreaker_OnlyAnUnbrokenRunOfFailuresOpensIt(t *testing.T) {
	cb := NewCircuitBreaker(DeliveryCircuitBreakerConfig())
	fail := func() error { return throttledError(0) }

	for i := 0; i < 10; i++ {
		_ = cb.Execute(fail)
		_ = cb.Execute(fail)
		require.NoError(t, cb.Execute(func() error { return nil }))
	}
	assert.Equal(t, StateClosed, cb.GetState())

	for i := 0; i < deliveryFailuresToOpen; i++ {
		_ = cb.Execute(fail)
	}
	assert.Equal(t, StateOpen, cb.GetState())
}

func TestDeliveryBreaker_FirstSuccessfulProbeClosesIt(t *testing.T) {
	config := DeliveryCircuitBreakerConfig()
	config.RecoveryTimeout = time.Nanosecond
	cb := NewCircuitBreaker(config)
	for i := 0; i < deliveryFailuresToOpen; i++ {
		_ = cb.Execute(func() error { return throttledError(0) })
	}

	require.NoError(t, cb.Execute(func() error { return nil }))

	assert.Equal(t, StateClosed, cb.GetState())
}

func TestExecuteHonoringRetryAfter_HoldsTheBreakerOpenForTheRequestedTime(t *testing.T) {
	cb := NewCircuitBreaker(DeliveryCircuitBreakerConfig())

	err := cb.ExecuteHonoringRetryAfter(func() error { return throttledError(200 * time.Millisecond) })

	require.Error(t, err)
	assert.Equal(t, StateOpen, cb.GetState())
	assert.ErrorIs(t, cb.Execute(func() error { return nil }), ErrCircuitOpen)
	assert.InDelta(t, float64(200*time.Millisecond), float64(cb.GetStats().TimeUntilRecovery), float64(50*time.Millisecond))
	time.Sleep(210 * time.Millisecond)
	assert.NoError(t, cb.Execute(func() error { return nil }))
}

func TestHoldOpenFor_NeverShortensAnExistingHold(t *testing.T) {
	cb := NewCircuitBreaker(DeliveryCircuitBreakerConfig())

	cb.HoldOpenFor(time.Minute)
	cb.HoldOpenFor(time.Second)

	assert.Greater(t, cb.GetStats().TimeUntilRecovery, 50*time.Second)
}

func TestWithRetry_WaitsForAShortRetryAfterAndStopsOnALongOne(t *testing.T) {
	config := &RetryConfig{MaxRetries: 3, BaseDelay: time.Millisecond, MaxDelay: 500 * time.Millisecond}

	calls := 0
	start := time.Now()
	err := WithRetry(context.Background(), func() error {
		calls++
		if calls == 1 {
			return throttledError(150 * time.Millisecond)
		}
		return nil
	}, config)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, time.Since(start), 150*time.Millisecond)

	calls = 0
	err = WithRetry(context.Background(), func() error {
		calls++
		return throttledError(time.Minute)
	}, config)
	require.Error(t, err)
	assert.Equal(t, 1, calls)
	assert.Equal(t, time.Minute, RetryAfterOf(err))
	assert.False(t, errors.Is(err, ErrCircuitOpen))
}

func TestCalculateDelay_RetryAfterWithJitterNeverExceedsMaxDelay(t *testing.T) {
	config := &RetryConfig{MaxRetries: 3, BaseDelay: time.Millisecond, MaxDelay: 2 * time.Second, JitterFactor: 0.5}

	for i := 0; i < 100; i++ {
		assert.LessOrEqual(t, calculateDelay(0, config, throttledError(2*time.Second)), config.MaxDelay)
	}
}

func TestExecute_ASlowCallFromBeforeTheBreakerOpenedCannotEndTheCurrentProbe(t *testing.T) {
	config := DeliveryCircuitBreakerConfig()
	config.RecoveryTimeout = time.Nanosecond
	cb := NewCircuitBreaker(config)
	releaseSlow := make(chan struct{})
	slowDone := make(chan struct{})
	slowStarted := make(chan struct{})
	go func() {
		defer close(slowDone)
		_ = cb.Execute(func() error {
			close(slowStarted)
			<-releaseSlow
			return throttledError(0)
		})
	}()
	<-slowStarted
	for i := 0; i < deliveryFailuresToOpen; i++ {
		_ = cb.Execute(func() error { return throttledError(0) })
	}
	require.Equal(t, StateOpen, cb.GetState())

	releaseProbe := make(chan struct{})
	probeDone := make(chan error, 1)
	probeStarted := make(chan struct{})
	go func() {
		probeDone <- cb.Execute(func() error {
			close(probeStarted)
			<-releaseProbe
			return nil
		})
	}()
	<-probeStarted
	close(releaseSlow)
	<-slowDone

	assert.Equal(t, StateHalfOpen, cb.GetState(), "the stale failure is ignored")
	assert.ErrorIs(t, cb.Execute(func() error { return nil }), ErrCircuitOpen, "the probe is still the only call let through")
	close(releaseProbe)
	require.NoError(t, <-probeDone)
	assert.Equal(t, StateClosed, cb.GetState())
}

func TestAcquireCircuitBreaker_SameHostSpelledDifferentlySharesOneBreaker(t *testing.T) {
	ResetCircuitBreakers()
	t.Cleanup(ResetCircuitBreakers)

	byDefault, releaseDefault := AcquireCircuitBreaker(MeteringEndpoint, "", "hak_key")
	byName, releaseByName := AcquireCircuitBreaker(MeteringEndpoint, " HTTPS://API.Revenium.ai/ ", "hak_key")
	byLegacyPath, releaseLegacy := AcquireCircuitBreaker(MeteringEndpoint, core.DefaultReveniumBaseURL+"/meter/v2", "hak_key")
	t.Cleanup(func() { releaseDefault(); releaseByName(); releaseLegacy() })

	assert.Same(t, byDefault, byName)
	assert.Same(t, byDefault, byLegacyPath)
}
