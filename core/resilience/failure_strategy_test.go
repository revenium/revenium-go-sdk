package resilience

import (
	"testing"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/stretchr/testify/assert"
)

func TestDefaultFailureStrategy_RetryableErrors(t *testing.T) {
	s := &DefaultFailureStrategy{}

	assert.True(t, s.IsRetryableError(core.NewNetworkError("timeout", nil)))
	assert.True(t, s.IsRetryableError(core.NewMeteringError("server error", nil)))
}

func TestDefaultFailureStrategy_ThrottledIsRetryable(t *testing.T) {
	s := &DefaultFailureStrategy{}

	err429 := core.NewNetworkError("rate limited", nil)
	err429.StatusCode = 429
	assert.True(t, s.IsRetryableError(err429))
}

func TestDefaultFailureStrategy_NonRetryableErrors(t *testing.T) {
	s := &DefaultFailureStrategy{}

	assert.False(t, s.IsRetryableError(core.NewValidationError("bad input", nil)))
	assert.False(t, s.IsRetryableError(core.NewConfigError("missing key", nil)))
	assert.False(t, s.IsRetryableError(core.NewAuthError("unauthorized", nil)))
}
