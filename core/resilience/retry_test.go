package resilience

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var fastConfig = &RetryConfig{
	MaxRetries:   3,
	BaseDelay:    1 * time.Millisecond,
	MaxDelay:     10 * time.Millisecond,
	JitterFactor: 0,
}

func TestWithRetry_SuccessFirstAttempt(t *testing.T) {
	var calls int32
	err := WithRetry(context.Background(), func() error {
		atomic.AddInt32(&calls, 1)
		return nil
	}, fastConfig)

	assert.NoError(t, err)
	assert.Equal(t, int32(1), atomic.LoadInt32(&calls))
}

func TestWithRetry_SuccessAfterRetries(t *testing.T) {
	var calls int32
	err := WithRetry(context.Background(), func() error {
		count := atomic.AddInt32(&calls, 1)
		if count < 3 {
			return core.NewNetworkError("transient", nil)
		}
		return nil
	}, fastConfig)

	assert.NoError(t, err)
	assert.Equal(t, int32(3), atomic.LoadInt32(&calls))
}

func TestWithRetry_NonRetryableFailsImmediately(t *testing.T) {
	var calls int32
	err := WithRetry(context.Background(), func() error {
		atomic.AddInt32(&calls, 1)
		return core.NewValidationError("bad input", nil)
	}, fastConfig)

	require.Error(t, err)
	assert.True(t, core.IsValidationError(err))
	assert.Equal(t, int32(1), atomic.LoadInt32(&calls))
}

func TestWithRetry_ExhaustsRetries(t *testing.T) {
	var calls int32
	err := WithRetry(context.Background(), func() error {
		atomic.AddInt32(&calls, 1)
		return core.NewNetworkError("persistent failure", nil)
	}, fastConfig)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed after 3 retries")
	assert.True(t, core.IsNetworkError(err))
	assert.Equal(t, int32(4), atomic.LoadInt32(&calls))
}

func TestWithRetry_ThrottledUsesLongerBackoff(t *testing.T) {
	config := &RetryConfig{
		MaxRetries:   1,
		BaseDelay:    50 * time.Millisecond,
		MaxDelay:     500 * time.Millisecond,
		JitterFactor: 0,
	}

	throttledErr := core.NewNetworkError("rate limited", nil)
	throttledErr.StatusCode = 429

	start := time.Now()
	var calls int32
	_ = WithRetry(context.Background(), func() error {
		atomic.AddInt32(&calls, 1)
		return throttledErr
	}, config)
	elapsed := time.Since(start)

	assert.Equal(t, int32(2), atomic.LoadInt32(&calls))
	assert.GreaterOrEqual(t, elapsed.Milliseconds(), int64(140))
}

func TestWithRetry_NilConfigUsesDefault(t *testing.T) {
	err := WithRetry(context.Background(), func() error {
		return nil
	}, nil)
	assert.NoError(t, err)
}

func TestWithRetry_ContextCanceledNotRetried(t *testing.T) {
	var calls int32
	err := WithRetry(context.Background(), func() error {
		atomic.AddInt32(&calls, 1)
		return context.Canceled
	}, fastConfig)

	require.Error(t, err)
	assert.Equal(t, int32(1), atomic.LoadInt32(&calls))
}

func TestWithRetry_ContextDeadlineExceededNotRetried(t *testing.T) {
	var calls int32
	err := WithRetry(context.Background(), func() error {
		atomic.AddInt32(&calls, 1)
		return context.DeadlineExceeded
	}, fastConfig)

	require.Error(t, err)
	assert.Equal(t, int32(1), atomic.LoadInt32(&calls))
}

func TestWithRetry_RespectsContextCancellationDuringSleep(t *testing.T) {
	config := &RetryConfig{
		MaxRetries:   3,
		BaseDelay:    5 * time.Second,
		MaxDelay:     5 * time.Second,
		JitterFactor: 0,
	}
	ctx, cancel := context.WithCancel(context.Background())

	var calls int32
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	err := WithRetry(ctx, func() error {
		atomic.AddInt32(&calls, 1)
		return core.NewNetworkError("fail", nil)
	}, config)
	elapsed := time.Since(start)

	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, int32(1), atomic.LoadInt32(&calls))
	assert.Less(t, elapsed, 1*time.Second)
}

func TestWithRetryResult_Success(t *testing.T) {
	result, err := WithRetryResult(context.Background(), func() (string, error) {
		return "ok", nil
	}, fastConfig)

	assert.NoError(t, err)
	assert.Equal(t, "ok", result)
}

func TestWithRetryResult_SuccessAfterRetries(t *testing.T) {
	var calls int32
	result, err := WithRetryResult(context.Background(), func() (int, error) {
		count := atomic.AddInt32(&calls, 1)
		if count < 2 {
			return 0, core.NewMeteringError("transient", nil)
		}
		return 42, nil
	}, fastConfig)

	assert.NoError(t, err)
	assert.Equal(t, 42, result)
	assert.Equal(t, int32(2), atomic.LoadInt32(&calls))
}

func TestWithRetryResult_NonRetryableFailsImmediately(t *testing.T) {
	var calls int32
	result, err := WithRetryResult(context.Background(), func() (string, error) {
		atomic.AddInt32(&calls, 1)
		return "", core.NewConfigError("bad config", nil)
	}, fastConfig)

	require.Error(t, err)
	assert.Equal(t, "", result)
	assert.Equal(t, int32(1), atomic.LoadInt32(&calls))
}

func TestWithRetryResult_ExhaustsRetries(t *testing.T) {
	var calls int32
	result, err := WithRetryResult(context.Background(), func() (int, error) {
		atomic.AddInt32(&calls, 1)
		return 0, core.NewNetworkError("fail", nil)
	}, fastConfig)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed after 3 retries")
	assert.True(t, core.IsNetworkError(err))
	assert.Equal(t, 0, result)
	assert.Equal(t, int32(4), atomic.LoadInt32(&calls))
}

func TestDefaultRetryConfig(t *testing.T) {
	config := DefaultRetryConfig()

	assert.Equal(t, 3, config.MaxRetries)
	assert.Equal(t, 1*time.Second, config.BaseDelay)
	assert.Equal(t, 5*time.Second, config.MaxDelay)
	assert.Equal(t, 0.1, config.JitterFactor)
}

func TestCalculateDelay_ExponentialBackoff(t *testing.T) {
	config := &RetryConfig{
		BaseDelay:    100 * time.Millisecond,
		MaxDelay:     1 * time.Second,
		JitterFactor: 0,
	}
	retryableErr := core.NewNetworkError("fail", nil)

	d0 := calculateDelay(0, config, retryableErr)
	d1 := calculateDelay(1, config, retryableErr)
	d2 := calculateDelay(2, config, retryableErr)

	assert.Equal(t, 100*time.Millisecond, d0)
	assert.Equal(t, 200*time.Millisecond, d1)
	assert.Equal(t, 400*time.Millisecond, d2)
}

func TestCalculateDelay_CapsAtMaxDelay(t *testing.T) {
	config := &RetryConfig{
		BaseDelay:    100 * time.Millisecond,
		MaxDelay:     300 * time.Millisecond,
		JitterFactor: 0,
	}
	retryableErr := core.NewNetworkError("fail", nil)

	d3 := calculateDelay(3, config, retryableErr)
	assert.Equal(t, 300*time.Millisecond, d3)
}

func TestCalculateDelay_ThrottledMultiplied(t *testing.T) {
	config := &RetryConfig{
		BaseDelay:    100 * time.Millisecond,
		MaxDelay:     1 * time.Second,
		JitterFactor: 0,
	}
	throttledErr := core.NewNetworkError("throttled", nil)
	throttledErr.StatusCode = 429

	d0 := calculateDelay(0, config, throttledErr)
	assert.Equal(t, 300*time.Millisecond, d0)
}

func TestCalculateDelay_ThrottledCappedAtMaxDelay(t *testing.T) {
	config := &RetryConfig{
		BaseDelay:    100 * time.Millisecond,
		MaxDelay:     300 * time.Millisecond,
		JitterFactor: 0,
	}
	throttledErr := core.NewNetworkError("throttled", nil)
	throttledErr.StatusCode = 429

	d2 := calculateDelay(2, config, throttledErr)
	assert.Equal(t, 300*time.Millisecond, d2)
}
