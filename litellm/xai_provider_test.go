package litellm

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/revenium/revenium-go-sdk/core/enforcement"
	"github.com/revenium/revenium-go-sdk/core/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newServedModelStub(t *testing.T, servedModel string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl-x","object":"chat.completion","created":1700000000,"model":"` + servedModel + `",` +
			`"choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}],` +
			`"usage":{"prompt_tokens":3,"completion_tokens":1,"total_tokens":4}}`))
	}))
	t.Cleanup(server.Close)
	return server
}

func TestExtractProvider_RecognisesXAIModels(t *testing.T) {
	for _, model := range []string{"xai/grok-4", "XAI/grok-3-mini", "grok-4", "grok-code-fast-1"} {
		t.Run(model, func(t *testing.T) {
			assert.Equal(t, "XAI", ExtractProvider(model))
			assert.Equal(t, "XAI", ExtractModelSource(model))
		})
	}
}

func TestExtractProvider_GroqPrefixStillWinsOverGrokName(t *testing.T) {
	assert.Equal(t, "Groq", ExtractProvider("groq/grok-like-model"))
}

func TestChatCompletions_RecordsGrokThroughProxyAsXAI(t *testing.T) {
	cases := []struct{ requested, served string }{
		{"xai/grok-4", "xai/grok-4"},
		{"xai/grok-4", "grok-4"},
		{"grok-4", "grok-4"},
	}
	for _, tc := range cases {
		t.Run(tc.requested+"->"+tc.served, func(t *testing.T) {
			proxy := newServedModelStub(t, tc.served)
			metering := testutil.NewMockMeteringServer()
			t.Cleanup(metering.Close)
			client := newEnforcedLiteLLM(t, proxy.URL, metering.URL(), metering.URL(), "")

			_, err := client.Chat().Completions().New(asSubscriber("go-bughunt@example.com"), chatRequest(tc.requested))
			require.NoError(t, err)

			require.True(t, metering.WaitForPayloads(1, 2*time.Second))
			payload := metering.GetPayloads()[0]
			assert.Equal(t, "XAI", payload["provider"])
			assert.Equal(t, "XAI", payload["modelSource"])
			assert.Equal(t, "grok-4", payload["model"])
		})
	}
}

func TestChatCompletions_XAICostRuleCoversGrokThroughProxy(t *testing.T) {
	enforcement.Stop()
	t.Cleanup(enforcement.Stop)
	provider := newProviderStub(t)
	rules := testutil.NewMockRulesServer()
	t.Cleanup(rules.Close)
	metering := testutil.NewMockMeteringServer()
	t.Cleanup(metering.Close)
	rules.SetPayload(enforcementTestTeam, breachedPerPersonRule(`{"dimension": "PROVIDER", "operator": "IS", "value": "XAI"}`))

	client := newEnforcedLiteLLM(t, provider.server.URL, rules.URL(), metering.URL(), enforcementTestTeam)
	completions := client.Chat().Completions()

	for _, model := range []string{"xai/grok-4", "grok-4"} {
		before := provider.hits.Load()
		_, err := completions.New(asSubscriber("alice@example.com"), chatRequest(model))
		var ece *enforcement.ErrCostLimitExceeded
		require.True(t, errors.As(err, &ece), "XAI rule did not block %s", model)
		assert.Equal(t, before, provider.hits.Load(), "%s reached the provider", model)
	}

	before := provider.hits.Load()
	_, err := completions.New(asSubscriber("alice@example.com"), chatRequest("openai/gpt-4.1-nano"))
	assert.NoError(t, err)
	assert.Equal(t, before+1, provider.hits.Load(), "XAI rule blocked an OpenAI call")
}
