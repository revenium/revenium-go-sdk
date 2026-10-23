package metering

import (
	"testing"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewMeteringClient_FollowsTheDebugSetting(t *testing.T) {
	cases := []struct {
		name      string
		envDebug  string
		cfgDebug  bool
		wantLevel core.LogLevel
	}{
		{name: "REVENIUM_DEBUG alone does not reach a config built by hand", envDebug: "true", wantLevel: core.LogLevelInfo},
		{name: "Debug in the config turns debug on without REVENIUM_DEBUG", cfgDebug: true, wantLevel: core.LogLevelDebug},
		{name: "neither leaves the level alone", wantLevel: core.LogLevelInfo},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			logger := useRecordingLogger(t)
			t.Cleanup(func() { core.SetGlobalDebug(false) })
			t.Setenv(core.EnvDebug, tc.envDebug)

			client, err := NewMeteringClient(MeteringClientConfig{APIKey: "hak_test_key_123", BaseURL: unreachableBaseURL(), Debug: tc.cfgDebug})
			require.NoError(t, err)
			t.Cleanup(func() { _ = client.Close() })

			assert.Equal(t, tc.wantLevel, logger.GetLevel())
		})
	}
}

func TestClientConfigFor_CarriesTheDebugSetting(t *testing.T) {
	t.Setenv(core.EnvDebug, "")
	assert.True(t, ClientConfigFor(&core.ReveniumConfig{Debug: true}).Debug)
	assert.False(t, ClientConfigFor(&core.ReveniumConfig{}).Debug)
}

func TestClientConfigFor_TakesREVENIUM_DEBUGForAConfigBuiltByHand(t *testing.T) {
	t.Setenv(core.EnvDebug, "true")
	assert.True(t, ClientConfigFor(&core.ReveniumConfig{}).Debug)
}

func TestNewMeteringClient_BuiltFromADebugReveniumConfigTurnsOnDebugWithoutTheEnvironment(t *testing.T) {
	logger := useRecordingLogger(t)
	t.Cleanup(func() { core.SetGlobalDebug(false) })
	t.Setenv(core.EnvDebug, "")
	cfg := ClientConfigFor(&core.ReveniumConfig{APIKey: "hak_test_key_123", BaseURL: unreachableBaseURL(), Debug: true})

	client, err := NewMeteringClient(cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })

	assert.Equal(t, core.LogLevelDebug, logger.GetLevel())
}
