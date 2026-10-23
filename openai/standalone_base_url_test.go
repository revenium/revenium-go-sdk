package openai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/revenium/revenium-go-sdk/core/enforcement"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStandaloneClient_SendsRecordsAndReadsRulesAtTheMeteringBaseURLSetting(t *testing.T) {
	enforcement.Stop()
	t.Cleanup(enforcement.Stop)
	provider := newProviderStub(t)
	var mu sync.Mutex
	var paths []string
	revenium := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths = append(paths, r.Method+" "+r.URL.Path)
		mu.Unlock()
		if r.Method == http.MethodGet {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(revenium.Close)
	t.Setenv(core.EnvBaseURL, revenium.URL)

	client, err := NewReveniumOpenAI(&Config{
		OpenAIAPIKey: "sk-test",
		BaseURL:      provider.server.URL,
		Revenium:     &core.ReveniumConfig{APIKey: "hak_customer_a", TeamID: "team-a"},
	})
	require.NoError(t, err)
	_, err = client.Chat().Completions().New(context.Background(), chatParams("gpt-4.1-nano"))
	require.NoError(t, err)
	require.NoError(t, client.Close())

	mu.Lock()
	defer mu.Unlock()
	assert.Contains(t, paths, "GET /profitstream/v2/api/ai/enforcement-rules/team-a")
	assert.Contains(t, paths, "POST /meter/v2/ai/completions")
}
