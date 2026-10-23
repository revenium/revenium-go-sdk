package grok

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

type grokStub struct {
	server *httptest.Server
	hits   atomic.Int32
}

func newGrokStub(t *testing.T) *grokStub {
	t.Helper()
	stub := &grokStub{}
	stub.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		stub.hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id": "chatcmpl-grok-enforced",
			"object": "chat.completion",
			"created": 1700000000,
			"model": "grok-4",
			"choices": [{"index": 0, "message": {"role": "assistant", "content": "hi"}, "finish_reason": "stop"}],
			"usage": {"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2}
		}`))
	}))
	t.Cleanup(stub.server.Close)
	return stub
}

func newEnforcedGrok(t *testing.T, grokURL, rulesURL, meteringURL, teamID string) *ReveniumGrok {
	t.Helper()
	r, err := NewReveniumGrok(&Config{
		XAIAPIKey: "xai-test",
		BaseURL:   grokURL,
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

func asSubscriber(email string) context.Context {
	return core.WithSubscriber(context.Background(), &core.Subscriber{Email: email})
}

func TestChatCompletions_PerPersonCapBlocksOnlyThePersonOverIt(t *testing.T) {
	enforcement.Stop()
	t.Cleanup(enforcement.Stop)
	provider := newGrokStub(t)
	rules := testutil.NewMockRulesServer()
	t.Cleanup(rules.Close)
	metering := testutil.NewMockMeteringServer()
	t.Cleanup(metering.Close)
	rules.SetPayload("team-a", `{"rules": [{"ruleId": 7, "name": "per-dev", "metricType": "TOTAL_COST", "threshold": 50,
		"currentValue": 61, "breached": true, "shadowMode": false, "action": "BLOCK", "groupBy": "SUBSCRIBER",
		"filters": [{"dimension": "PROVIDER", "operator": "IS", "value": "xai"}],
		"groupBreakdown": [
			{"groupValue": "alice@example.com", "currentValue": 61, "breached": true},
			{"groupValue": "bob@example.com", "currentValue": 3, "breached": false}]}]}`)

	client := newEnforcedGrok(t, provider.server.URL, rules.URL(), metering.URL(), "team-a")

	_, err := client.ChatCompletions(asSubscriber("Alice@Example.com"), ChatCompletionRequest{
		Model:    "grok-4",
		Messages: []ChatMessage{{Role: "user", Content: "hi"}},
	})
	var ece *enforcement.ErrCostLimitExceeded
	require.True(t, errors.As(err, &ece), "alice should be blocked, got %v", err)
	assert.Equal(t, int32(0), provider.hits.Load())

	_, err = client.ChatCompletions(asSubscriber("bob@example.com"), ChatCompletionRequest{
		Model:    "grok-4",
		Messages: []ChatMessage{{Role: "user", Content: "hi"}},
	})
	require.NoError(t, err)
	assert.Equal(t, int32(1), provider.hits.Load())
}

func TestChatCompletions_EachClientEnforcesItsOwnTeamsRules(t *testing.T) {
	enforcement.Stop()
	t.Cleanup(enforcement.Stop)
	provider := newGrokStub(t)
	rules := testutil.NewMockRulesServer()
	t.Cleanup(rules.Close)
	metering := testutil.NewMockMeteringServer()
	t.Cleanup(metering.Close)
	rules.SetPayload("team-b", `{"rules": [{"ruleId": 4211, "threshold": 10, "currentValue": 12, "breached": true,
		"shadowMode": false, "action": "BLOCK", "filters": []}]}`)

	unblocked := newEnforcedGrok(t, provider.server.URL, rules.URL(), metering.URL(), "team-a")
	blockedTeam := newEnforcedGrok(t, provider.server.URL, rules.URL(), metering.URL(), "team-b")

	_, err := unblocked.ChatCompletions(asSubscriber("alice@example.com"), ChatCompletionRequest{
		Model:    "grok-4",
		Messages: []ChatMessage{{Role: "user", Content: "hi"}},
	})
	require.NoError(t, err)

	_, err = blockedTeam.ChatCompletions(asSubscriber("alice@example.com"), ChatCompletionRequest{
		Model:    "grok-4",
		Messages: []ChatMessage{{Role: "user", Content: "hi"}},
	})
	var ece *enforcement.ErrCostLimitExceeded
	require.True(t, errors.As(err, &ece), "team-b's client should be blocked, got %v", err)
	assert.Equal(t, "4211", ece.RuleID)
	assert.Equal(t, int32(1), provider.hits.Load())
}

func TestChatCompletions_RuleOnCanonicalXAIBlocksGrokCalls(t *testing.T) {
	for _, ruleProvider := range []string{"XAI", "xai", "xAI"} {
		t.Run(ruleProvider, func(t *testing.T) {
			enforcement.Stop()
			t.Cleanup(enforcement.Stop)
			provider := newGrokStub(t)
			rules := testutil.NewMockRulesServer()
			t.Cleanup(rules.Close)
			metering := testutil.NewMockMeteringServer()
			t.Cleanup(metering.Close)
			rules.SetPayload("team-x", `{"rules": [{"ruleId": 9, "threshold": 10, "currentValue": 12, "breached": true,
				"shadowMode": false, "action": "BLOCK",
				"filters": [{"dimension": "PROVIDER", "operator": "IS", "value": "`+ruleProvider+`"}]}]}`)

			client := newEnforcedGrok(t, provider.server.URL, rules.URL(), metering.URL(), "team-x")

			_, err := client.ChatCompletions(context.Background(), ChatCompletionRequest{
				Model:    "grok-4",
				Messages: []ChatMessage{{Role: "user", Content: "hi"}},
			})
			var ece *enforcement.ErrCostLimitExceeded
			require.True(t, errors.As(err, &ece), "a BLOCK rule on provider %q should stop the call, got %v", ruleProvider, err)
			assert.Equal(t, int32(0), provider.hits.Load())
		})
	}
}
