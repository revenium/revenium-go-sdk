package runway

import (
	"errors"

	"github.com/revenium/revenium-go-sdk/core"
)

// ErrorTypeTask is a Runway-specific error type for task polling errors
const ErrorTypeTask core.ErrorType = "TASK_ERROR"

// NewTaskError creates a new task polling error
func NewTaskError(message string, err error) *core.ReveniumError {
	return &core.ReveniumError{
		Type:    ErrorTypeTask,
		Message: message,
		Err:     err,
	}
}

// IsTaskError checks if an error is a task polling error
func IsTaskError(err error) bool {
	var revErr *core.ReveniumError
	return errors.As(err, &revErr) && revErr.Type == ErrorTypeTask
}
