package core

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestReveniumError(t *testing.T) {
	t.Run("Error without cause", func(t *testing.T) {
		err := &ReveniumError{
			Type:    ErrorTypeConfig,
			Message: "test error",
			Err:     nil,
		}

		assert.Equal(t, "[CONFIG_ERROR] test error", err.Error())
	})

	t.Run("Error with cause", func(t *testing.T) {
		cause := errors.New("underlying error")
		err := &ReveniumError{
			Type:    ErrorTypeConfig,
			Message: "test error",
			Err:     cause,
		}

		assert.Contains(t, err.Error(), "test error")
		assert.Contains(t, err.Error(), "underlying error")
	})

	t.Run("Unwrap", func(t *testing.T) {
		cause := errors.New("underlying error")
		err := &ReveniumError{
			Type:    ErrorTypeConfig,
			Message: "test error",
			Err:     cause,
		}

		assert.Equal(t, cause, err.Unwrap())
	})

	t.Run("Error with StatusCode", func(t *testing.T) {
		err := &ReveniumError{
			Type:       ErrorTypeNetwork,
			Message:    "network error",
			StatusCode: 500,
		}

		assert.Equal(t, 500, err.GetStatusCode())
	})

	t.Run("Error with Details", func(t *testing.T) {
		details := map[string]interface{}{
			"key1": "value1",
			"key2": 123,
		}
		err := &ReveniumError{
			Type:    ErrorTypeValidation,
			Message: "validation error",
			Details: details,
		}

		assert.Equal(t, details, err.GetDetails())
	})

	t.Run("WithDetails", func(t *testing.T) {
		err := &ReveniumError{
			Type:    ErrorTypeConfig,
			Message: "test error",
		}

		err.WithDetails("field", "value")
		err.WithDetails("count", 123)

		assert.Equal(t, "value", err.Details["field"])
		assert.Equal(t, 123, err.Details["count"])
	})

	t.Run("Is method", func(t *testing.T) {
		err1 := &ReveniumError{
			Type:    ErrorTypeConfig,
			Message: "config error",
		}
		err2 := &ReveniumError{
			Type:    ErrorTypeConfig,
			Message: "another config error",
		}
		err3 := &ReveniumError{
			Type:    ErrorTypeNetwork,
			Message: "network error",
		}

		assert.True(t, err1.Is(err2))
		assert.False(t, err1.Is(err3))
	})
}

func TestNewErrors(t *testing.T) {
	cause := errors.New("cause")

	tests := []struct {
		name      string
		newFunc   func(string, error) *ReveniumError
		errType   ErrorType
		checkFunc func(error) bool
	}{
		{
			name:      "ConfigError",
			newFunc:   NewConfigError,
			errType:   ErrorTypeConfig,
			checkFunc: IsConfigError,
		},
		{
			name:      "ProviderError",
			newFunc:   NewProviderError,
			errType:   ErrorTypeProvider,
			checkFunc: IsProviderError,
		},
		{
			name:      "MeteringError",
			newFunc:   NewMeteringError,
			errType:   ErrorTypeMetering,
			checkFunc: IsMeteringError,
		},
		{
			name:      "NetworkError",
			newFunc:   NewNetworkError,
			errType:   ErrorTypeNetwork,
			checkFunc: IsNetworkError,
		},
		{
			name:      "ValidationError",
			newFunc:   NewValidationError,
			errType:   ErrorTypeValidation,
			checkFunc: IsValidationError,
		},
		{
			name:      "AuthError",
			newFunc:   NewAuthError,
			errType:   ErrorTypeAuth,
			checkFunc: IsAuthError,
		},
		{
			name:      "StreamingError",
			newFunc:   NewStreamingError,
			errType:   ErrorTypeStreaming,
			checkFunc: IsStreamingError,
		},
		{
			name:      "InternalError",
			newFunc:   NewInternalError,
			errType:   ErrorTypeInternal,
			checkFunc: IsInternalError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.newFunc("test message", cause)

			assert.Equal(t, tt.errType, err.Type)
			assert.Equal(t, "test message", err.Message)
			assert.Equal(t, cause, err.Err)
			assert.True(t, tt.checkFunc(err))
		})
	}
}

func TestIsErrorType(t *testing.T) {
	configErr := NewConfigError("config error", nil)
	providerErr := NewProviderError("provider error", nil)
	regularErr := errors.New("regular error")

	t.Run("IsConfigError", func(t *testing.T) {
		assert.True(t, IsConfigError(configErr))
		assert.False(t, IsConfigError(providerErr))
		assert.False(t, IsConfigError(regularErr))
	})

	t.Run("IsProviderError", func(t *testing.T) {
		assert.True(t, IsProviderError(providerErr))
		assert.False(t, IsProviderError(configErr))
		assert.False(t, IsProviderError(regularErr))
	})

	t.Run("IsMeteringError", func(t *testing.T) {
		meteringErr := NewMeteringError("metering error", nil)
		assert.True(t, IsMeteringError(meteringErr))
		assert.False(t, IsMeteringError(configErr))
	})

	t.Run("IsNetworkError", func(t *testing.T) {
		networkErr := NewNetworkError("network error", nil)
		assert.True(t, IsNetworkError(networkErr))
		assert.False(t, IsNetworkError(configErr))
	})

	t.Run("IsValidationError", func(t *testing.T) {
		validationErr := NewValidationError("validation error", nil)
		assert.True(t, IsValidationError(validationErr))
		assert.False(t, IsValidationError(configErr))
	})

	t.Run("IsAuthError", func(t *testing.T) {
		authErr := NewAuthError("auth error", nil)
		assert.True(t, IsAuthError(authErr))
		assert.False(t, IsAuthError(configErr))
	})

	t.Run("IsStreamingError", func(t *testing.T) {
		streamingErr := NewStreamingError("streaming error", nil)
		assert.True(t, IsStreamingError(streamingErr))
		assert.False(t, IsStreamingError(configErr))
	})

	t.Run("IsInternalError", func(t *testing.T) {
		internalErr := NewInternalError("internal error", nil)
		assert.True(t, IsInternalError(internalErr))
		assert.False(t, IsInternalError(configErr))
	})
}
