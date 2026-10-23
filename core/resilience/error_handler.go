package resilience

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/revenium/revenium-go-sdk/core"
)

type ErrorClassification int

const (
	ClassificationRetryable ErrorClassification = iota
	ClassificationNonRetryable
	ClassificationThrottled
)

func (c ErrorClassification) String() string {
	switch c {
	case ClassificationRetryable:
		return "RETRYABLE"
	case ClassificationNonRetryable:
		return "NON_RETRYABLE"
	case ClassificationThrottled:
		return "THROTTLED"
	default:
		return "UNKNOWN"
	}
}

func ClassifyError(err error) ErrorClassification {
	if err == nil || errors.Is(err, ErrCircuitOpen) {
		return ClassificationNonRetryable
	}

	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || isHTTPClientTimeout(err) {
		return ClassificationNonRetryable
	}

	var revErr *core.ReveniumError
	if errors.As(err, &revErr) && revErr.StatusCode != 0 {
		return ClassifyHTTPResponse(revErr.StatusCode, revErr.Message)
	}

	if isThrottlingError(err) {
		return ClassificationThrottled
	}

	if core.IsValidationError(err) || core.IsConfigError(err) || core.IsAuthError(err) {
		return ClassificationNonRetryable
	}

	if core.IsNetworkError(err) || core.IsMeteringError(err) {
		return ClassificationRetryable
	}

	if isRawNetworkError(err) {
		return ClassificationRetryable
	}

	return ClassificationNonRetryable
}

func ClassifyHTTPResponse(statusCode int, _ string) ErrorClassification {
	switch {
	case statusCode >= 200 && statusCode < 300:
		return ClassificationNonRetryable
	case statusCode == 408:
		return ClassificationRetryable
	case statusCode == 429:
		return ClassificationThrottled
	case statusCode >= 500:
		return ClassificationRetryable
	default:
		return ClassificationNonRetryable
	}
}

func IsRetryable(c ErrorClassification) bool {
	return c == ClassificationRetryable || c == ClassificationThrottled
}

func IsNetworkErrorCheck(err error) bool {
	if err == nil {
		return false
	}

	if core.IsNetworkError(err) {
		return true
	}

	return isRawNetworkError(err)
}

func IsThrottlingErrorCheck(err error) bool {
	if err == nil {
		return false
	}

	var revErr *core.ReveniumError
	if errors.As(err, &revErr) && revErr.StatusCode == 429 {
		return true
	}

	return isThrottlingError(err)
}

func IsConfigErrorCheck(err error) bool {
	if err == nil {
		return false
	}

	if core.IsConfigError(err) || core.IsAuthError(err) {
		return true
	}

	var revErr *core.ReveniumError
	if errors.As(err, &revErr) {
		return revErr.StatusCode == 401 || revErr.StatusCode == 403
	}

	return false
}

// httpClientTimeoutMarker is the text net/http puts in the error when an
// http.Client's Timeout expires, on every Go release the SDK supports. Go 1.23
// made that error match context.DeadlineExceeded; on Go 1.21 and 1.22 it is
// only a net.Error whose Timeout() is true. A dial or read timeout from the
// network itself also reports Timeout() but never carries the marker, and
// stays a retryable network error as it is on Go 1.23+.
const httpClientTimeoutMarker = "Client.Timeout"

func isHTTPClientTimeout(err error) bool {
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout() && strings.Contains(netErr.Error(), httpClientTimeoutMarker)
}

func isRawNetworkError(err error) bool {
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}

	errStr := strings.ToLower(err.Error())
	patterns := []string{
		"timeout",
		"connection refused",
		"connection reset",
		"no such host",
		"econnreset",
		"econnrefused",
	}
	for _, p := range patterns {
		if strings.Contains(errStr, p) {
			return true
		}
	}
	return false
}

func isThrottlingError(err error) bool {
	errStr := strings.ToLower(err.Error())
	patterns := []string{
		"rate limit",
		"rate_limit",
		"overloaded",
		"throttl",
		"too many requests",
	}
	for _, p := range patterns {
		if strings.Contains(errStr, p) {
			return true
		}
	}
	return false
}

// ResponseError maps a non-2xx answer from Revenium to the error type the
// retry, breaker and buffer logic act on, carrying any Retry-After it sent.
func ResponseError(label string, statusCode int, header http.Header, body []byte) *core.ReveniumError {
	msg := fmt.Sprintf("%s API returned %d: %s", label, statusCode, string(body))
	var revErr *core.ReveniumError
	switch ClassifyHTTPResponse(statusCode, string(body)) {
	case ClassificationThrottled:
		revErr = core.NewNetworkError(msg, nil).WithDetails("throttled", true)
	case ClassificationRetryable:
		revErr = core.NewMeteringError(msg, nil)
	default:
		revErr = core.NewValidationError(msg, nil)
	}
	revErr.StatusCode = statusCode
	revErr.RetryAfter = ParseRetryAfter(header.Get("Retry-After"), time.Now())
	return revErr
}
