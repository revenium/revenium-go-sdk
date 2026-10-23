package resilience

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/revenium/revenium-go-sdk/core"
)

// MaxRetryAfter bounds how long one Retry-After header can hold deliveries
// back, so a malformed or hostile value cannot park records until they expire.
const MaxRetryAfter = 5 * time.Minute

// ParseRetryAfter reads a Retry-After header in either RFC 9110 form,
// delta-seconds or an HTTP date, and returns 0 when it is absent or unusable.
func ParseRetryAfter(header string, now time.Time) time.Duration {
	value := strings.TrimSpace(header)
	if value == "" {
		return 0
	}
	var wait time.Duration
	if seconds, err := strconv.Atoi(value); err == nil {
		wait = time.Duration(seconds) * time.Second
	} else if at, err := http.ParseTime(value); err == nil {
		wait = at.Sub(now)
	}
	if wait <= 0 {
		return 0
	}
	if wait > MaxRetryAfter {
		return MaxRetryAfter
	}
	return wait
}

func RetryAfterOf(err error) time.Duration {
	var revErr *core.ReveniumError
	if errors.As(err, &revErr) {
		return revErr.RetryAfter
	}
	return 0
}

// ExecuteHonoringRetryAfter runs fn through the breaker and keeps the
// breaker open for as long as the server asked, so no other request with the
// same credentials goes out before then.
func (cb *CircuitBreaker) ExecuteHonoringRetryAfter(fn func() error) error {
	err := cb.Execute(fn)
	cb.HoldOpenFor(RetryAfterOf(err))
	return err
}
