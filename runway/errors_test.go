package runway

import (
	"errors"
	"testing"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/stretchr/testify/assert"
)

func TestNewTaskError(t *testing.T) {
	cause := errors.New("polling failed")
	err := NewTaskError("task timed out", cause)
	assert.Equal(t, ErrorTypeTask, err.Type)
	assert.Equal(t, "task timed out", err.Message)
	assert.Equal(t, cause, err.Err)
}

func TestIsTaskError_True(t *testing.T) {
	assert.True(t, IsTaskError(NewTaskError("x", nil)))
}

func TestIsTaskError_False(t *testing.T) {
	assert.False(t, IsTaskError(errors.New("plain")))
	assert.False(t, IsTaskError(core.NewConfigError("x", nil)))
	assert.False(t, IsTaskError(nil))
}
