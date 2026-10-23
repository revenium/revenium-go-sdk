package openai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	openaisdk "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/shared/constant"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/revenium/revenium-go-sdk/core/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func unsetEnv(t *testing.T, names ...string) {
	t.Helper()
	for _, name := range names {
		t.Setenv(name, "")
		require.NoError(t, os.Unsetenv(name))
	}
}

func envFile(entries map[string]string) string {
	var b strings.Builder
	for name, value := range entries {
		b.WriteString(name + "=" + value + "\n")
	}
	return b.String()
}

func TestInitialize_SettingsFileAboveTheWorkingFolderIsNeverLoaded(t *testing.T) {
	resetSharedClient(t)
	var attackerRequests atomic.Int32
	attacker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attackerRequests.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(attacker.Close)
	openaiServer := httptest.NewServer(chatCompletionHandler())
	t.Cleanup(openaiServer.Close)
	metering := testutil.NewMockMeteringServer()
	t.Cleanup(metering.Close)

	testutil.WorkBelowEnvFile(t, envFile(map[string]string{
		"OPENAI_BASE_URL":          attacker.URL,
		core.EnvBaseURL:            attacker.URL,
		core.EnvEnforcementBaseURL: attacker.URL,
		core.EnvTeamID:             "team-from-parent-folder",
		"OPENAI_ORG_ID":            "org-from-parent-folder",
	}))
	unsetEnv(t, core.EnvEnforcementBaseURL, core.EnvEnforcementAPIKey, core.EnvTeamID, "OPENAI_ORG_ID", core.EnvEnvFilesDisable)
	t.Setenv("OPENAI_BASE_URL", openaiServer.URL)
	t.Setenv(core.EnvBaseURL, metering.URL())
	t.Setenv(core.EnvAPIKey, "hak_shell_key")
	t.Setenv("OPENAI_API_KEY", "sk-shell-key")

	require.NoError(t, Initialize(WithAzureDisabled(true)))
	client, err := GetClient()
	require.NoError(t, err)
	_, err = client.Chat().Completions().New(context.Background(), openaisdk.ChatCompletionNewParams{
		Model: "gpt-4.1-nano",
		Messages: []openaisdk.ChatCompletionMessageParamUnion{
			{OfUser: &openaisdk.ChatCompletionUserMessageParam{
				Role:    constant.ValueOf[constant.User](),
				Content: openaisdk.ChatCompletionUserMessageParamContentUnion{OfString: openaisdk.String("hi")},
			}},
		},
	})
	require.NoError(t, err)
	client.Flush()

	require.True(t, metering.WaitForPayloads(1, 2*time.Second))
	assert.Never(t, func() bool { return attackerRequests.Load() > 0 }, 300*time.Millisecond, 20*time.Millisecond,
		"the parent file's cost-control address, which is unset in the shell, must never receive the Revenium key")
	assert.Empty(t, os.Getenv(core.EnvTeamID))
	assert.Empty(t, os.Getenv(core.EnvEnforcementBaseURL), "the cost-control address in the parent file would carry the Revenium key")
	assert.Empty(t, os.Getenv("OPENAI_ORG_ID"))
	assert.Empty(t, client.config.OpenAIOrgID)
}

func TestLoadFromEnv_ValuesSetInCodeWinOverTheWorkingFolderEnvFile(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(dir+"/.env", []byte(envFile(map[string]string{
		"OPENAI_ORG_ID": "org-from-file",
		core.EnvBaseURL: "http://from-file.invalid",
	})), 0o600))
	testutil.Chdir(t, dir)
	unsetEnv(t, "OPENAI_ORG_ID", core.EnvBaseURL, core.EnvEnvFilesDisable)

	cfg := &Config{OpenAIOrgID: "org-in-code", Revenium: &core.ReveniumConfig{BaseURL: "https://in-code.example"}}
	require.NoError(t, cfg.loadFromEnv())

	assert.Equal(t, "org-in-code", cfg.OpenAIOrgID)
	assert.Equal(t, "https://in-code.example", cfg.Revenium.BaseURL)
}

func TestLoadFromEnv_ReadsTheWorkingFolderEnvFileForValuesNotSetInCode(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(dir+"/.env", []byte("OPENAI_ORG_ID=org-from-file\n"), 0o600))
	testutil.Chdir(t, dir)
	unsetEnv(t, "OPENAI_ORG_ID", core.EnvEnvFilesDisable)

	cfg := &Config{OpenAIAPIKey: "sk-in-code", Revenium: &core.ReveniumConfig{APIKey: "hak_in_code"}}
	require.NoError(t, cfg.loadFromEnv())

	assert.Equal(t, "org-from-file", cfg.OpenAIOrgID)
}
