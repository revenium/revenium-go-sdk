package resilience

import (
	"context"
	"errors"
	"fmt"
	"net"
	"testing"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/stretchr/testify/assert"
)

func TestClassifyHTTPResponse_StatusCodes(t *testing.T) {
	tests := []struct {
		status int
		want   ErrorClassification
	}{
		{200, ClassificationNonRetryable},
		{201, ClassificationNonRetryable},
		{400, ClassificationNonRetryable},
		{401, ClassificationNonRetryable},
		{403, ClassificationNonRetryable},
		{404, ClassificationNonRetryable},
		{408, ClassificationRetryable},
		{429, ClassificationThrottled},
		{500, ClassificationRetryable},
		{502, ClassificationRetryable},
		{503, ClassificationRetryable},
		{504, ClassificationRetryable},
		{505, ClassificationRetryable},
		{507, ClassificationRetryable},
		{529, ClassificationRetryable},
		{418, ClassificationNonRetryable},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%d", tt.status), func(t *testing.T) {
			got := ClassifyHTTPResponse(tt.status, "")
			assert.Equal(t, tt.want, got, "status %d", tt.status)
		})
	}
}

func TestClassifyError_ReveniumErrorTypes(t *testing.T) {
	assert.Equal(t, ClassificationRetryable, ClassifyError(core.NewNetworkError("timeout", nil)))
	assert.Equal(t, ClassificationRetryable, ClassifyError(core.NewMeteringError("server error", nil)))
	assert.Equal(t, ClassificationNonRetryable, ClassifyError(core.NewValidationError("bad input", nil)))
	assert.Equal(t, ClassificationNonRetryable, ClassifyError(core.NewConfigError("missing key", nil)))
	assert.Equal(t, ClassificationNonRetryable, ClassifyError(core.NewAuthError("unauthorized", nil)))
	assert.Equal(t, ClassificationNonRetryable, ClassifyError(nil))
}

func TestClassifyError_WithStatusCode(t *testing.T) {
	err429 := core.NewNetworkError("rate limited", nil)
	err429.StatusCode = 429
	assert.Equal(t, ClassificationThrottled, ClassifyError(err429))

	err500 := core.NewMeteringError("server error", nil)
	err500.StatusCode = 500
	assert.Equal(t, ClassificationRetryable, ClassifyError(err500))

	err400 := core.NewValidationError("bad request", nil)
	err400.StatusCode = 400
	assert.Equal(t, ClassificationNonRetryable, ClassifyError(err400))
}

func TestClassifyError_ContextErrors(t *testing.T) {
	assert.Equal(t, ClassificationNonRetryable, ClassifyError(context.Canceled))
	assert.Equal(t, ClassificationNonRetryable, ClassifyError(context.DeadlineExceeded))
}

func TestClassifyError_ThrottlingPatterns(t *testing.T) {
	assert.Equal(t, ClassificationThrottled, ClassifyError(errors.New("rate limit exceeded")))
	assert.Equal(t, ClassificationThrottled, ClassifyError(errors.New("too many requests")))
	assert.Equal(t, ClassificationThrottled, ClassifyError(errors.New("service overloaded")))
	assert.Equal(t, ClassificationThrottled, ClassifyError(errors.New("throttling exception")))
}

func TestIsRetryable(t *testing.T) {
	assert.True(t, IsRetryable(ClassificationRetryable))
	assert.True(t, IsRetryable(ClassificationThrottled))
	assert.False(t, IsRetryable(ClassificationNonRetryable))
}

func TestIsNetworkErrorCheck(t *testing.T) {
	assert.True(t, IsNetworkErrorCheck(core.NewNetworkError("timeout", nil)))
	assert.True(t, IsNetworkErrorCheck(errors.New("connection refused")))
	assert.True(t, IsNetworkErrorCheck(errors.New("connection reset by peer")))
	assert.True(t, IsNetworkErrorCheck(errors.New("no such host")))
	assert.True(t, IsNetworkErrorCheck(&net.DNSError{Err: "lookup failed", Name: "example.com"}))
	assert.False(t, IsNetworkErrorCheck(errors.New("invalid input")))
	assert.False(t, IsNetworkErrorCheck(nil))
}

func TestIsThrottlingErrorCheck(t *testing.T) {
	err429 := core.NewNetworkError("rate limited", nil)
	err429.StatusCode = 429
	assert.True(t, IsThrottlingErrorCheck(err429))
	assert.True(t, IsThrottlingErrorCheck(errors.New("rate limit exceeded")))
	assert.True(t, IsThrottlingErrorCheck(errors.New("too many requests")))
	assert.False(t, IsThrottlingErrorCheck(errors.New("server error")))
	assert.False(t, IsThrottlingErrorCheck(nil))
}

func TestIsConfigErrorCheck(t *testing.T) {
	assert.True(t, IsConfigErrorCheck(core.NewConfigError("missing key", nil)))
	assert.True(t, IsConfigErrorCheck(core.NewAuthError("unauthorized", nil)))

	err401 := &core.ReveniumError{Type: core.ErrorTypeProvider, StatusCode: 401}
	assert.True(t, IsConfigErrorCheck(err401))

	err403 := &core.ReveniumError{Type: core.ErrorTypeProvider, StatusCode: 403}
	assert.True(t, IsConfigErrorCheck(err403))

	assert.False(t, IsConfigErrorCheck(errors.New("generic error")))
	assert.False(t, IsConfigErrorCheck(nil))
}

func TestErrorClassification_String(t *testing.T) {
	assert.Equal(t, "RETRYABLE", ClassificationRetryable.String())
	assert.Equal(t, "NON_RETRYABLE", ClassificationNonRetryable.String())
	assert.Equal(t, "THROTTLED", ClassificationThrottled.String())
}
