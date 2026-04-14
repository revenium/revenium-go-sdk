package fal

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestStatus_DefaultEnabled(t *testing.T) {
	client := newTestFal(t, "http://localhost:1")
	defer client.Close()
	status := client.GetStatus()
	assert.True(t, status.Initialized)
	assert.True(t, status.Enabled)
	assert.True(t, status.HasConfig)
	assert.Equal(t, "http://localhost:1", status.BaseURL)
}

func TestStatus_EnableDisableCycle(t *testing.T) {
	client := newTestFal(t, "http://localhost:1")
	defer client.Close()
	assert.True(t, client.IsEnabled())
	client.Disable()
	assert.False(t, client.IsEnabled())
	client.Enable()
	assert.True(t, client.IsEnabled())
}

func TestStatus_NilConfigDoesNotPanic(t *testing.T) {
	r := &ReveniumFal{}
	assert.NotPanics(t, func() {
		s := r.GetStatus()
		assert.False(t, s.Initialized)
		assert.False(t, s.HasConfig)
		assert.Equal(t, "", s.BaseURL)
	})
}

func TestStatus_GlobalWhenNotInitialized(t *testing.T) {
	Reset()
	s := GetStatus()
	assert.False(t, s.Initialized)
	assert.False(t, s.Enabled)
}
