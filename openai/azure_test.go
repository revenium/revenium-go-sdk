package openai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	openaisdk "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/azure"
	"github.com/openai/openai-go/v3/option"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/revenium/revenium-go-sdk/core/metering"
	"github.com/revenium/revenium-go-sdk/core/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newAzureTestOpenAI(t *testing.T, azureURL string, azureClient *http.Client, meteringURL string) *ReveniumOpenAI {
	t.Helper()
	cfg := &Config{
		AzureAPIKey:     "azure-key",
		AzureEndpoint:   azureURL,
		AzureAPIVersion: "2024-06-01",
		Revenium: &core.ReveniumConfig{
			APIKey:  "hak_test_key_123",
			BaseURL: meteringURL,
		},
	}
	mc, err := metering.NewMeteringClient(metering.MeteringClientConfig{
		APIKey:  cfg.Revenium.APIKey,
		BaseURL: cfg.Revenium.BaseURL,
	})
	require.NoError(t, err)

	client := openaisdk.NewClient(
		azure.WithEndpoint(cfg.AzureEndpoint, cfg.AzureAPIVersion),
		azure.WithAPIKey(cfg.AzureAPIKey),
		option.WithHTTPClient(azureClient),
	)
	r := &ReveniumOpenAI{client: client, config: cfg, provider: ProviderAzure, metering: mc}
	t.Cleanup(func() { _ = r.Close() })
	return r
}

func TestAzureChatCompletion_MetersTheDeploymentNameNotTheServedModel(t *testing.T) {
	azureServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl-1","object":"chat.completion","created":1700000000,"model":"gpt-4o-2024-11-20",` +
			`"choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}],` +
			`"usage":{"prompt_tokens":3,"completion_tokens":1,"total_tokens":4}}`))
	}))
	defer azureServer.Close()

	mock := testutil.NewMockMeteringServer()
	defer mock.Close()

	r := newAzureTestOpenAI(t, azureServer.URL, azureServer.Client(), mock.URL())

	resp, err := r.Chat().Completions().New(context.Background(), chatParams("my-deployment"))
	require.NoError(t, err)
	require.Equal(t, "gpt-4o-2024-11-20", resp.Model)
	r.Flush()

	require.True(t, mock.WaitForPayloads(1, 2*time.Second))
	payload := mock.GetPayloads()[0]
	assert.Equal(t, "my-deployment", payload["model"])
	assert.Equal(t, "AZURE", payload["provider"])
}
