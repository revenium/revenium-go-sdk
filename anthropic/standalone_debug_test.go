package anthropic

import (
	"testing"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/revenium/revenium-go-sdk/core/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStandaloneConstructor_DebugSettingTurnsOnDebugLogging(t *testing.T) {
	cases := []struct {
		name     string
		envDebug string
		cfgDebug bool
	}{
		{name: "REVENIUM_DEBUG=true", envDebug: "true"},
		{name: "Config.Revenium.Debug", cfgDebug: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			logger := core.GetLogger()
			original := logger.GetLevel()
			t.Cleanup(func() {
				core.SetGlobalDebug(false)
				logger.SetLevel(original)
			})
			logger.SetLevel(core.LogLevelInfo)
			t.Setenv(core.EnvLogLevel, "")
			t.Setenv(core.EnvDebug, tc.envDebug)
			meteringServer := testutil.NewMockMeteringServer()
			t.Cleanup(meteringServer.Close)

			client, err := NewReveniumAnthropic(&Config{
				AnthropicAPIKey: "sk-ant-test", BedrockDisabled: true,
				Revenium: &core.ReveniumConfig{APIKey: "hak_test_key_123", BaseURL: meteringServer.URL(), Debug: tc.cfgDebug},
			})
			require.NoError(t, err)
			t.Cleanup(func() { _ = client.Close() })

			assert.Equal(t, core.LogLevelDebug, logger.GetLevel())
		})
	}
}

func TestStandaloneConstructor_AClientWithoutDebugKeepsDebugAnEarlierClientTurnedOn(t *testing.T) {
	logger := core.GetLogger()
	original := logger.GetLevel()
	t.Cleanup(func() {
		core.SetGlobalDebug(false)
		logger.SetLevel(original)
	})
	logger.SetLevel(core.LogLevelInfo)
	t.Setenv(core.EnvLogLevel, "")
	t.Setenv(core.EnvDebug, "")
	meteringServer := testutil.NewMockMeteringServer()
	t.Cleanup(meteringServer.Close)

	for _, debug := range []bool{true, false} {
		client, err := NewReveniumAnthropic(&Config{
			AnthropicAPIKey: "sk-ant-test", BedrockDisabled: true,
			Revenium: &core.ReveniumConfig{APIKey: "hak_test_key_123", BaseURL: meteringServer.URL(), Debug: debug},
		})
		require.NoError(t, err)
		t.Cleanup(func() { _ = client.Close() })
	}

	core.InitializeLogger()
	assert.Equal(t, core.LogLevelDebug, logger.GetLevel(), "the debug request of the first client survives the second client")
}
