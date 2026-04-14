package litellm

import (
	"testing"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestClient(t *testing.T) *ReveniumLiteLLM {
	t.Helper()
	client, err := NewReveniumLiteLLM(&Config{
		LiteLLMProxyURL: "http://localhost:4000",
		Revenium:        &core.ReveniumConfig{APIKey: "hak_test_key_123"},
	})
	require.NoError(t, err)
	return client
}

func TestStatus_DefaultEnabled(t *testing.T) {
	client := newTestClient(t)
	status := client.GetStatus()
	assert.True(t, status.Enabled)
	assert.True(t, status.Initialized)
	assert.True(t, status.HasConfig)
	assert.Equal(t, "http://localhost:4000", status.ProxyURL)
}

func TestStatus_EnableDisableCycle(t *testing.T) {
	client := newTestClient(t)
	assert.True(t, client.IsEnabled())

	client.Disable()
	assert.False(t, client.IsEnabled())
	assert.False(t, client.GetStatus().Enabled)

	client.Enable()
	assert.True(t, client.IsEnabled())
	assert.True(t, client.GetStatus().Enabled)
}

func TestStatus_GlobalWhenNotInitialized(t *testing.T) {
	ResetGlobalState()
	status := GetStatus()
	assert.False(t, status.Initialized)
	assert.False(t, status.Enabled)
	assert.False(t, status.HasConfig)
}
