package anthropic

import (
	"errors"
	"net"
	"strings"

	"github.com/revenium/revenium-go-sdk/core"
)

type AnthropicFailureStrategy struct{}

func (s *AnthropicFailureStrategy) IsRetryableError(err error) bool {
	if err == nil {
		return false
	}

	if s.IsProviderThrottling(err) {
		return true
	}

	var revErr *core.ReveniumError
	if errors.As(err, &revErr) && revErr.StatusCode >= 500 {
		return true
	}

	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}

	errStr := strings.ToLower(err.Error())
	retryablePatterns := []string{
		"overloaded_error",
		"api_error",
		"timeout",
		"connection refused",
		"connection reset",
		"eof",
	}
	for _, p := range retryablePatterns {
		if strings.Contains(errStr, p) {
			return true
		}
	}

	return false
}

func (s *AnthropicFailureStrategy) IsProviderThrottling(err error) bool {
	if err == nil {
		return false
	}

	var revErr *core.ReveniumError
	if errors.As(err, &revErr) {
		if revErr.StatusCode == 429 || revErr.StatusCode == 529 {
			return true
		}
	}

	errStr := strings.ToLower(err.Error())
	throttlePatterns := []string{
		"rate_limit_error",
		"rate limit",
		"too many requests",
		"overloaded",
	}
	for _, p := range throttlePatterns {
		if strings.Contains(errStr, p) {
			return true
		}
	}

	return false
}
