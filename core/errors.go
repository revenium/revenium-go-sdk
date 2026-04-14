package core

import (
	"errors"
	"fmt"
)

// ErrorType represents the type of error that occurred
type ErrorType string

const (
	// Configuration errors
	ErrorTypeConfig ErrorType = "CONFIG_ERROR"

	// Metering errors
	ErrorTypeMetering ErrorType = "METERING_ERROR"

	// Provider errors (OpenAI, Azure, Anthropic, etc.)
	ErrorTypeProvider ErrorType = "PROVIDER_ERROR"

	// Authentication errors
	ErrorTypeAuth ErrorType = "AUTH_ERROR"

	// Network/HTTP errors
	ErrorTypeNetwork ErrorType = "NETWORK_ERROR"

	// Streaming errors
	ErrorTypeStreaming ErrorType = "STREAMING_ERROR"

	// Validation errors
	ErrorTypeValidation ErrorType = "VALIDATION_ERROR"

	// Internal errors
	ErrorTypeInternal ErrorType = "INTERNAL_ERROR"
)

// ReveniumError is the base error type for all Revenium middleware errors
type ReveniumError struct {
	Type       ErrorType
	Message    string
	Err        error
	StatusCode int
	Details    map[string]interface{}
}

// Error implements the error interface
func (e *ReveniumError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("[%s] %s: %v", e.Type, e.Message, e.Err)
	}
	return fmt.Sprintf("[%s] %s", e.Type, e.Message)
}

// Unwrap returns the underlying error
func (e *ReveniumError) Unwrap() error {
	return e.Err
}

// Is checks if the error is of a specific type
func (e *ReveniumError) Is(target error) bool {
	t, ok := target.(*ReveniumError)
	if !ok {
		return false
	}
	return e.Type == t.Type
}

// GetStatusCode returns the HTTP status code associated with the error
func (e *ReveniumError) GetStatusCode() int {
	if e.StatusCode != 0 {
		return e.StatusCode
	}

	// Default status codes based on error type
	switch e.Type {
	case ErrorTypeConfig, ErrorTypeValidation:
		return 400
	case ErrorTypeAuth:
		return 401
	case ErrorTypeProvider:
		return 502
	case ErrorTypeNetwork:
		return 503
	case ErrorTypeMetering:
		return 500
	default:
		return 500
	}
}

// WithDetails adds details to the error
func (e *ReveniumError) WithDetails(key string, value interface{}) *ReveniumError {
	if e.Details == nil {
		e.Details = make(map[string]interface{})
	}
	e.Details[key] = value
	return e
}

// GetDetails returns the error details
func (e *ReveniumError) GetDetails() map[string]interface{} {
	if e.Details == nil {
		return make(map[string]interface{})
	}
	return e.Details
}

// NewConfigError creates a new configuration error
func NewConfigError(message string, err error) *ReveniumError {
	return &ReveniumError{
		Type:    ErrorTypeConfig,
		Message: message,
		Err:     err,
	}
}

// NewMeteringError creates a new metering error
func NewMeteringError(message string, err error) *ReveniumError {
	return &ReveniumError{
		Type:    ErrorTypeMetering,
		Message: message,
		Err:     err,
	}
}

// NewProviderError creates a new provider error
func NewProviderError(message string, err error) *ReveniumError {
	return &ReveniumError{
		Type:    ErrorTypeProvider,
		Message: message,
		Err:     err,
	}
}

// NewAuthError creates a new authentication error
func NewAuthError(message string, err error) *ReveniumError {
	return &ReveniumError{
		Type:    ErrorTypeAuth,
		Message: message,
		Err:     err,
	}
}

// NewNetworkError creates a new network error
func NewNetworkError(message string, err error) *ReveniumError {
	return &ReveniumError{
		Type:    ErrorTypeNetwork,
		Message: message,
		Err:     err,
	}
}

// NewStreamingError creates a new streaming error
func NewStreamingError(message string, err error) *ReveniumError {
	return &ReveniumError{
		Type:    ErrorTypeStreaming,
		Message: message,
		Err:     err,
	}
}

// NewValidationError creates a new validation error
func NewValidationError(message string, err error) *ReveniumError {
	return &ReveniumError{
		Type:    ErrorTypeValidation,
		Message: message,
		Err:     err,
	}
}

// NewInternalError creates a new internal error
func NewInternalError(message string, err error) *ReveniumError {
	return &ReveniumError{
		Type:    ErrorTypeInternal,
		Message: message,
		Err:     err,
	}
}

// IsConfigError checks if an error is a configuration error
func IsConfigError(err error) bool {
	var revErr *ReveniumError
	return errors.As(err, &revErr) && revErr.Type == ErrorTypeConfig
}

// IsMeteringError checks if an error is a metering error
func IsMeteringError(err error) bool {
	var revErr *ReveniumError
	return errors.As(err, &revErr) && revErr.Type == ErrorTypeMetering
}

// IsProviderError checks if an error is a provider error
func IsProviderError(err error) bool {
	var revErr *ReveniumError
	return errors.As(err, &revErr) && revErr.Type == ErrorTypeProvider
}

// IsAuthError checks if an error is an authentication error
func IsAuthError(err error) bool {
	var revErr *ReveniumError
	return errors.As(err, &revErr) && revErr.Type == ErrorTypeAuth
}

// IsNetworkError checks if an error is a network error
func IsNetworkError(err error) bool {
	var revErr *ReveniumError
	return errors.As(err, &revErr) && revErr.Type == ErrorTypeNetwork
}

// IsStreamingError checks if an error is a streaming error
func IsStreamingError(err error) bool {
	var revErr *ReveniumError
	return errors.As(err, &revErr) && revErr.Type == ErrorTypeStreaming
}

// IsValidationError checks if an error is a validation error
func IsValidationError(err error) bool {
	var revErr *ReveniumError
	return errors.As(err, &revErr) && revErr.Type == ErrorTypeValidation
}

// IsInternalError checks if an error is an internal error
func IsInternalError(err error) bool {
	var revErr *ReveniumError
	return errors.As(err, &revErr) && revErr.Type == ErrorTypeInternal
}

// IsReveniumError checks if an error is a ReveniumError
func IsReveniumError(err error) bool {
	var revErr *ReveniumError
	return errors.As(err, &revErr)
}
