package resilience

import (
	"context"
	"errors"
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
	// RetryOnly, when set, is a hard filter: a failure it refuses is never
	// retried, whatever RetryAlso or the classifier say. A write that must not
	// be applied twice uses it to retry only the answers that prove the
	// request was not applied.
	RetryOnly func(error) bool
	// RetryAlso, when set, widens retries to a failure the classifier would
	// not retry, such as a request timeout on a call that is safe to repeat.
	// It is asked only for failures the classifier refuses, and only after an
	// open breaker, a Retry-After longer than MaxDelay and RetryOnly have all
	// allowed a retry; none of them can be overridden by it.
	RetryAlso func(error) bool
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

		if !config.retries(err) {
			return zero, err
		}
	}

	return zero, fmt.Errorf("failed after %d retries: %w", config.MaxRetries, lastErr)
}

func (c *RetryConfig) retries(err error) bool {
	if errors.Is(err, ErrCircuitOpen) || RetryAfterOf(err) > c.MaxDelay {
		return false
	}
	if c.RetryOnly != nil && !c.RetryOnly(err) {
		return false
	}
	if ClassifyError(err) != ClassificationNonRetryable {
		return true
	}
	return c.RetryAlso != nil && c.RetryAlso(err)
}

const throttleMultiplier = 3

func calculateDelay(attempt int, config *RetryConfig, lastErr error) time.Duration {
	if wait := RetryAfterOf(lastErr); wait > 0 {
		return min(wait+time.Duration(rand.Float64()*config.JitterFactor*float64(wait)), config.MaxDelay)
	}
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

// RetryThroughBreaker sends every attempt through the breaker, so an outage
// opens it after a few requests instead of after every call has used up all
// of its retries, and a Retry-After answer holds the breaker open for as long
// as the server asked. Each attempt takes its place in limit before asking the
// breaker, so an attempt that waited is still stopped by a breaker that
// opened in the meantime.
//
// When the breaker is open the call stops at once, without another wait, and
// returns an error that matches ErrCircuitOpen and still carries the last
// failure the call itself saw, so callers keep the server's status and body.
func RetryThroughBreaker(cb *CircuitBreaker, limit *ConcurrencyLimit, config *RetryConfig, fn func() error) error {
	if config == nil {
		config = DefaultRetryConfig()
	}
	var lastFailure error
	return WithRetry(context.Background(), func() error {
		err := limit.Run(func() error { return cb.ExecuteHonoringRetryAfter(fn) })
		switch {
		case err == nil:
			return nil
		case errors.Is(err, ErrCircuitOpen):
			if lastFailure != nil {
				return &circuitOpenError{cause: lastFailure}
			}
			return err
		}
		lastFailure = err
		if !cb.CanExecute() && RetryAfterOf(err) == 0 {
			return &circuitOpenError{cause: err}
		}
		return err
	}, config)
}
