package openai

import (
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/revenium/revenium-go-sdk/core/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInitialize_AddressesLeftUnsetAreNeverTakenFromTheParentFolder(t *testing.T) {
	resetSharedClient(t)
	var attackerRequests atomic.Int32
	attacker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attackerRequests.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(attacker.Close)

	testutil.WorkBelowEnvFile(t, envFile(map[string]string{
		"OPENAI_BASE_URL":          attacker.URL,
		core.EnvBaseURL:            attacker.URL,
		core.EnvEnforcementBaseURL: attacker.URL,
		core.EnvTeamID:             "team-from-parent-folder",
	}))
	unsetEnv(t, "OPENAI_BASE_URL", core.EnvBaseURL, core.EnvEnforcementBaseURL, core.EnvEnforcementAPIKey,
		core.EnvTeamID, core.EnvEnvFilesDisable)
	t.Setenv(core.EnvAPIKey, "hak_shell_key")
	t.Setenv("OPENAI_API_KEY", "sk-shell-key")

	require.NoError(t, Initialize(WithAzureDisabled(true)))
	client, err := GetClient()
	require.NoError(t, err)

	assert.Empty(t, os.Getenv("OPENAI_BASE_URL"), "the parent file's OpenAI address would receive every prompt")
	assert.Empty(t, os.Getenv(core.EnvBaseURL), "the parent file's Revenium address would receive the metering key and every record")
	assert.Empty(t, client.config.effectiveBaseURL())
	assert.Equal(t, core.DefaultBaseURL, client.config.Revenium.BaseURL)
	assert.Never(t, func() bool { return attackerRequests.Load() > 0 }, 300*time.Millisecond, 20*time.Millisecond)
}
