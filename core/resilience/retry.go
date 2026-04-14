package resilience

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"time"

	"github.com/revenium/revenium-go-sdk/core"
)

type RetryConfig struct {
	MaxRetries   int
	BaseDelay    time.Duration
	MaxDelay     time.Duration
	JitterFactor float64
}

func DefaultRetryConfig() *RetryConfig {
	return &RetryConfig{
		MaxRetries:   3,
		BaseDelay:    1 * time.Second,
		MaxDelay:     5 * time.Second,
		JitterFactor: 0.1,
	}
}

func WithRetry(ctx context.Context, fn func() error, config *RetryConfig) error {
	_, err := withRetryInternal(ctx, func() (struct{}, error) {
		return struct{}{}, fn()
	}, config)
	return err
}

func WithRetryResult[T any](ctx context.Context, fn func() (T, error), config *RetryConfig) (T, error) {
	return withRetryInternal(ctx, fn, config)
}

func withRetryInternal[T any](ctx context.Context, fn func() (T, error), config *RetryConfig) (T, error) {
	if config == nil {
		config = DefaultRetryConfig()
	}

	var lastErr error
	var zero T

	for attempt := 0; attempt <= config.MaxRetries; attempt++ {
		if attempt > 0 {
			delay := calculateDelay(attempt-1, config, lastErr)
			core.Debug("[RETRY] attempt %d/%d after %v delay", attempt, config.MaxRetries, delay)
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return zero, ctx.Err()
			}
		}

		select {
		case <-ctx.Done():
			return zero, ctx.Err()
		default:
		}

		result, err := fn()
		if err == nil {
			return result, nil
		}

		lastErr = err

		if ClassifyError(err) == ClassificationNonRetryable {
			return zero, err
		}
	}

	return zero, fmt.Errorf("failed after %d retries: %w", config.MaxRetries, lastErr)
}

const throttleMultiplier = 3

func calculateDelay(attempt int, config *RetryConfig, lastErr error) time.Duration {
	delay := float64(config.BaseDelay) * math.Pow(2, float64(attempt))
	if delay > float64(config.MaxDelay) {
		delay = float64(config.MaxDelay)
	}

	if ClassifyError(lastErr) == ClassificationThrottled {
		delay *= throttleMultiplier
	}

	if delay > float64(config.MaxDelay) {
		delay = float64(config.MaxDelay)
	}

	jitter := rand.Float64() * config.JitterFactor * delay
	return time.Duration(delay + jitter)
}
