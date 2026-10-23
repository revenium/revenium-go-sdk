package openai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	openaisdk "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/revenium/revenium-go-sdk/core/metering"
	"github.com/revenium/revenium-go-sdk/core/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDetectProvider_XAIBaseURL(t *testing.T) {
	cases := []struct {
		name     string
		config   *Config
		expected Provider
	}{
		{name: "xAI API host", config: &Config{BaseURL: "https://api.x.ai/v1"}, expected: ProviderXAI},
		{name: "xAI apex domain", config: &Config{BaseURL: "https://x.ai/v1"}, expected: ProviderXAI},
		{name: "xAI host with a trailing dot and upper case", config: &Config{BaseURL: "https://API.X.AI./v1"}, expected: ProviderXAI},
		{name: "a domain that only ends in x.ai", config: &Config{BaseURL: "https://notx.ai/v1"}, expected: ProviderOpenAI},
		{name: "x.ai as a subdomain of another host", config: &Config{BaseURL: "https://x.ai.example.com/v1"}, expected: ProviderOpenAI},
		{name: "x.ai only in the path", config: &Config{BaseURL: "https://proxy.example.com/x.ai/v1"}, expected: ProviderOpenAI},
		{name: "Azure credentials win over an xAI base URL", config: &Config{BaseURL: "https://api.x.ai/v1", AzureAPIKey: "k", AzureEndpoint: "https://r.openai.azure.com"}, expected: ProviderAzure},
		{name: "xAI with Azure detection off", config: &Config{BaseURL: "https://api.x.ai/v1", AzureDisabled: true, AzureAPIKey: "k", AzureEndpoint: "https://r.openai.azure.com"}, expected: ProviderXAI},
		{name: "Azure base URL is unchanged", config: &Config{BaseURL: "https://r.openai.azure.com"}, expected: ProviderAzure},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, DetectProvider(tc.config))
		})
	}
}

func TestDetectProvider_XAIBaseURLFromTheEnvironment(t *testing.T) {
	t.Setenv("OPENAI_BASE_URL", "https://api.x.ai/v1")

	assert.Equal(t, ProviderXAI, DetectProvider(&Config{}))
	assert.Equal(t, ProviderOpenAI, DetectProvider(&Config{BaseURL: "https://api.openai.com/v1"}), "an explicit BaseURL wins, as it does in the client")
}

func TestDetectProvider_OpenAIBaseURLFromTheEnvironmentStaysOpenAI(t *testing.T) {
	t.Setenv("OPENAI_BASE_URL", "https://api.openai.com/v1")

	assert.Equal(t, ProviderOpenAI, DetectProvider(&Config{}))
}

func TestNewReveniumOpenAI_XAIBaseURLIsAnXAIClient(t *testing.T) {
	r, err := NewReveniumOpenAI(&Config{
		OpenAIAPIKey: "xai-key",
		BaseURL:      "https://api.x.ai/v1",
		Revenium:     &core.ReveniumConfig{APIKey: "hak_test_key_123", BaseURL: "http://127.0.0.1:1"},
	})
	require.NoError(t, err)
	defer r.Close()

	assert.Equal(t, ProviderXAI, r.GetProvider())
}

func newXAITestOpenAI(t *testing.T, providerServer *httptest.Server, meteringURL string) *ReveniumOpenAI {
	t.Helper()
	mc, err := metering.NewMeteringClient(metering.MeteringClientConfig{APIKey: "hak_test_key_123", BaseURL: meteringURL})
	require.NoError(t, err)
	client := openaisdk.NewClient(option.WithAPIKey("xai-key"), option.WithBaseURL(providerServer.URL))
	r := &ReveniumOpenAI{client: client, config: &Config{}, provider: ProviderXAI, metering: mc}
	t.Cleanup(func() { _ = r.Close() })
	return r
}

func TestXAIChat_IsRecordedAsXAI(t *testing.T) {
	calls := map[string]func(r *ReveniumOpenAI) error{
		"sync": func(r *ReveniumOpenAI) error {
			_, err := r.Chat().Completions().New(context.Background(), chatParams("grok-4"))
			return err
		},
		"streamed": func(r *ReveniumOpenAI) error {
			stream, err := r.Chat().Completions().NewStreaming(context.Background(), chatParams("grok-4"))
			if err != nil {
				return err
			}
			for stream.Next() {
			}
			return stream.Close()
		},
	}
	handlers := map[string]func(t *testing.T) *httptest.Server{
		"sync":     func(t *testing.T) *httptest.Server { return httptest.NewServer(chatCompletionHandler()) },
		"streamed": func(t *testing.T) *httptest.Server { return httptest.NewServer(sseChatHandler(t)) },
	}
	servedModels := map[string]string{"sync": "gpt-4.1-nano", "streamed": "gpt-4o-mini"}
	for name, call := range calls {
		t.Run(name, func(t *testing.T) {
			providerServer := handlers[name](t)
			defer providerServer.Close()
			mock := testutil.NewMockMeteringServer()
			defer mock.Close()
			r := newXAITestOpenAI(t, providerServer, mock.URL())

			require.NoError(t, call(r))
			r.Flush()

			record := assertSingleRecordAfterTimeout(t, mock)
			assert.Equal(t, "XAI", record["provider"])
			assert.Equal(t, "XAI", record["modelSource"])
			assert.Equal(t, servedModels[name], record["model"], "the record carries the model the stub served")
		})
	}
}

func TestOpenAIRecords_StillCarryNoModelSource(t *testing.T) {
	record := meteredChatPayload(t, context.Background())

	assert.Equal(t, "OPENAI", record["provider"])
	assert.NotContains(t, record, "modelSource")
}

func reasoningChatHandler(usage string) http.Handler {
	return jsonResponder(`{"id":"chatcmpl-x","object":"chat.completion","created":1700000000,"model":"grok-4",` +
		`"choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}],"usage":` + usage + `}`)
}

func TestXAIChat_BillsReasoningThatCompletionTokensLeaveOut(t *testing.T) {
	cases := []struct {
		name       string
		provider   Provider
		usage      string
		wantOutput float64
	}{
		{
			name:       "xAI reasoning shape: total includes reasoning outside completion",
			provider:   ProviderXAI,
			usage:      `{"prompt_tokens":100,"completion_tokens":20,"total_tokens":420,"completion_tokens_details":{"reasoning_tokens":300}}`,
			wantOutput: 320,
		},
		{
			name:       "xAI OpenAI shape: completion already holds reasoning",
			provider:   ProviderXAI,
			usage:      `{"prompt_tokens":100,"completion_tokens":320,"total_tokens":420,"completion_tokens_details":{"reasoning_tokens":300}}`,
			wantOutput: 320,
		},
		{
			name:       "OpenAI never adds reasoning to output",
			provider:   ProviderOpenAI,
			usage:      `{"prompt_tokens":100,"completion_tokens":20,"total_tokens":420,"completion_tokens_details":{"reasoning_tokens":300}}`,
			wantOutput: 20,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			providerServer := httptest.NewServer(reasoningChatHandler(tc.usage))
			defer providerServer.Close()
			mock := testutil.NewMockMeteringServer()
			defer mock.Close()
			r := newXAITestOpenAI(t, providerServer, mock.URL())
			r.provider = tc.provider

			_, err := r.Chat().Completions().New(context.Background(), chatParams("grok-4"))
			require.NoError(t, err)
			r.Flush()

			record := assertSingleRecordAfterTimeout(t, mock)
			assert.Equal(t, tc.wantOutput, record["outputTokenCount"])
			assert.Equal(t, float64(300), record["reasoningTokenCount"])
			assert.Equal(t, float64(420), record["totalTokenCount"])
		})
	}
}
