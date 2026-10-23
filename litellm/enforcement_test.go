package litellm

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/revenium/revenium-go-sdk/core/enforcement"
	"github.com/revenium/revenium-go-sdk/core/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const enforcementTestTeam = "team-a"

type providerStub struct {
	server *httptest.Server
	hits   atomic.Int32
}

func newProviderStub(t *testing.T) *providerStub {
	t.Helper()
	stub := &providerStub{}
	stub.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if answeredModelInfo(w, r, noModelRoutes) {
			return
		}
		stub.hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl-1","object":"chat.completion","created":1700000000,"model":"openai/gpt-4.1-nano",` +
			`"choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}],` +
			`"usage":{"prompt_tokens":3,"completion_tokens":1,"total_tokens":4}}`))
	}))
	t.Cleanup(stub.server.Close)
	return stub
}

func newProviderSSEStub(t *testing.T) *providerStub {
	t.Helper()
	stub := &providerStub{}
	stub.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if answeredModelInfo(w, r, noModelRoutes) {
			return
		}
		stub.hits.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		lines := []string{
			`data: {"id":"chatcmpl-1","object":"chat.completion.chunk","created":1700000000,"model":"openai/gpt-4.1-nano","choices":[{"index":0,"delta":{"role":"assistant","content":"hi"},"finish_reason":null}]}` + "\n\n",
			`data: {"id":"chatcmpl-1","object":"chat.completion.chunk","created":1700000000,"model":"openai/gpt-4.1-nano","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":1,"total_tokens":4}}` + "\n\n",
			"data: [DONE]\n\n",
		}
		for _, line := range lines {
			_, _ = w.Write([]byte(line))
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		}
	}))
	t.Cleanup(stub.server.Close)
	return stub
}

func newEnforcedLiteLLM(t *testing.T, proxyURL, rulesURL, meteringURL, teamID string) *ReveniumLiteLLM {
	t.Helper()
	r, err := NewReveniumLiteLLM(&Config{
		LiteLLMProxyURL: proxyURL,
		LiteLLMAPIKey:   "sk-litellm-test",
		Revenium: &core.ReveniumConfig{
			APIKey:             "hak_test_key_123",
			BaseURL:            meteringURL,
			EnforcementBaseURL: rulesURL,
			TeamID:             teamID,
		},
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = r.Close() })
	return r
}

func chatRequest(model string) ChatCompletionRequest {
	return ChatCompletionRequest{
		Model:    model,
		Messages: []ChatMessage{{Role: "user", Content: "hi"}},
	}
}

func asSubscriber(email string) context.Context {
	return core.WithSubscriber(context.Background(), &core.Subscriber{Email: email})
}

func breachedPerPersonRule(filters string) string {
	return `{"rules": [{"ruleId": 7, "name": "cap", "threshold": 50, "currentValue": 61, "periodType": "MONTHLY",
		"breached": true, "shadowMode": false, "action": "BLOCK", "metricType": "TOTAL_COST", "groupBy": "SUBSCRIBER",
		"filters": [` + filters + `],
		"groupBreakdown": [
			{"groupValue": "alice@example.com", "currentValue": 61, "breached": true},
			{"groupValue": "bob@example.com", "currentValue": 3, "breached": false}]}]}`
}

func TestChatCompletions_PerPersonCapBlocksOnlyThePersonOverIt(t *testing.T) {
	enforcement.Stop()
	t.Cleanup(enforcement.Stop)
	provider := newProviderStub(t)
	rules := testutil.NewMockRulesServer()
	t.Cleanup(rules.Close)
	metering := testutil.NewMockMeteringServer()
	t.Cleanup(metering.Close)
	rules.SetPayload(enforcementTestTeam, breachedPerPersonRule(`{"dimension": "PROVIDER", "operator": "IS", "value": "OpenAI"}`))

	client := newEnforcedLiteLLM(t, provider.server.URL, rules.URL(), metering.URL(), enforcementTestTeam)
	completions := client.Chat().Completions()

	before := provider.hits.Load()
	_, err := completions.New(asSubscriber("alice@example.com"), chatRequest("openai/gpt-4.1-nano"))
	var ece *enforcement.ErrCostLimitExceeded
	require.True(t, errors.As(err, &ece))
	assert.Equal(t, 61.0, ece.CurrentValue)
	assert.Equal(t, before, provider.hits.Load(), "alice reached the provider")

	before = provider.hits.Load()
	_, err = completions.New(asSubscriber("bob@example.com"), chatRequest("openai/gpt-4.1-nano"))
	assert.NoError(t, err)
	assert.Equal(t, before+1, provider.hits.Load(), "bob did not reach the provider")
}

func TestChatCompletionsStreaming_PerPersonCapBlocksOnlyThePersonOverIt(t *testing.T) {
	enforcement.Stop()
	t.Cleanup(enforcement.Stop)
	provider := newProviderSSEStub(t)
	rules := testutil.NewMockRulesServer()
	t.Cleanup(rules.Close)
	metering := testutil.NewMockMeteringServer()
	t.Cleanup(metering.Close)
	rules.SetPayload(enforcementTestTeam, breachedPerPersonRule(`{"dimension": "PROVIDER", "operator": "IS", "value": "OpenAI"}`))

	client := newEnforcedLiteLLM(t, provider.server.URL, rules.URL(), metering.URL(), enforcementTestTeam)
	completions := client.Chat().Completions()

	before := provider.hits.Load()
	_, err := completions.NewStreaming(asSubscriber("alice@example.com"), chatRequest("openai/gpt-4.1-nano"))
	var ece *enforcement.ErrCostLimitExceeded
	require.True(t, errors.As(err, &ece))
	assert.Equal(t, 61.0, ece.CurrentValue)
	assert.Equal(t, before, provider.hits.Load(), "alice reached the provider")

	before = provider.hits.Load()
	stream, err := completions.NewStreaming(asSubscriber("bob@example.com"), chatRequest("openai/gpt-4.1-nano"))
	require.NoError(t, err)
	for stream.Next() {
	}
	require.NoError(t, stream.Err())
	require.NoError(t, stream.Close())
	assert.Equal(t, before+1, provider.hits.Load(), "bob did not reach the provider")
}

func newEmbeddingsStub(t *testing.T) *providerStub {
	t.Helper()
	stub := &providerStub{}
	stub.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if answeredModelInfo(w, r, noModelRoutes) {
			return
		}
		stub.hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"object":"list","model":"openai/text-embedding-3-small",` +
			`"data":[{"object":"embedding","embedding":[0.1,0.2],"index":0}],` +
			`"usage":{"prompt_tokens":2,"total_tokens":2}}`))
	}))
	t.Cleanup(stub.server.Close)
	return stub
}

func TestEmbeddings_PerPersonCapBlocksOnlyThePersonOverIt(t *testing.T) {
	enforcement.Stop()
	t.Cleanup(enforcement.Stop)
	provider := newEmbeddingsStub(t)
	rules := testutil.NewMockRulesServer()
	t.Cleanup(rules.Close)
	metering := testutil.NewMockMeteringServer()
	t.Cleanup(metering.Close)
	rules.SetPayload(enforcementTestTeam, breachedPerPersonRule(`{"dimension": "PROVIDER", "operator": "IS", "value": "OpenAI"}`))

	client := newEnforcedLiteLLM(t, provider.server.URL, rules.URL(), metering.URL(), enforcementTestTeam)
	embeddings := client.Embeddings()
	req := EmbeddingRequest{Model: "openai/text-embedding-3-small", Input: "hi"}

	_, err := embeddings.New(asSubscriber("alice@example.com"), req)
	var ece *enforcement.ErrCostLimitExceeded
	require.True(t, errors.As(err, &ece))
	assert.Equal(t, int32(0), provider.hits.Load(), "alice reached the provider")

	_, err = embeddings.New(asSubscriber("bob@example.com"), req)
	require.NoError(t, err)
	assert.Equal(t, int32(1), provider.hits.Load(), "bob did not reach the provider")
}

func TestChatCompletions_ModelCapMatchesExtractedModelName(t *testing.T) {
	enforcement.Stop()
	t.Cleanup(enforcement.Stop)
	provider := newProviderStub(t)
	rules := testutil.NewMockRulesServer()
	t.Cleanup(rules.Close)
	metering := testutil.NewMockMeteringServer()
	t.Cleanup(metering.Close)
	rules.SetPayload(enforcementTestTeam, breachedPerPersonRule(`{"dimension": "MODEL", "operator": "IS", "value": "gpt-4.1-nano"}`))

	client := newEnforcedLiteLLM(t, provider.server.URL, rules.URL(), metering.URL(), enforcementTestTeam)
	completions := client.Chat().Completions()

	_, err := completions.New(asSubscriber("alice@example.com"), chatRequest("openai/gpt-4.1-nano"))
	var ece *enforcement.ErrCostLimitExceeded
	require.True(t, errors.As(err, &ece), "prefixed model should match the extracted-name rule, got %v", err)
	assert.Equal(t, int32(0), provider.hits.Load())
}
