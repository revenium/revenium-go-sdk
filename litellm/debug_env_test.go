package litellm

import (
	"testing"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadFromEnv_DebugSettingTurnsOnDebugLogging(t *testing.T) {
	logger := core.GetLogger()
	original := logger.GetLevel()
	t.Cleanup(func() {
		core.SetGlobalDebug(false)
		logger.SetLevel(original)
	})
	logger.SetLevel(core.LogLevelInfo)
	t.Setenv(core.EnvEnvFilesDisable, "true")
	t.Setenv(core.EnvLogLevel, "")
	t.Setenv(core.EnvDebug, "true")

	require.NoError(t, (&Config{}).loadFromEnv())

	assert.Equal(t, core.LogLevelDebug, logger.GetLevel())
}
