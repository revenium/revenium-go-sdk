package groq

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

type groqStub struct {
	server *httptest.Server
	hits   atomic.Int32
}

func newGroqStub(t *testing.T) *groqStub {
	t.Helper()
	stub := &groqStub{}
	stub.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		stub.hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id": "chatcmpl-groq-enforcement",
			"object": "chat.completion",
			"created": 1700000000,
			"model": "openai/gpt-oss-120b",
			"choices": [{
				"index": 0,
				"message": {"role": "assistant", "content": "hi"},
				"finish_reason": "stop"
			}],
			"usage": {"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2}
		}`))
	}))
	t.Cleanup(stub.server.Close)
	return stub
}

func newEnforcedGroq(t *testing.T, groqURL, rulesURL, meteringURL, teamID string) *ReveniumGroq {
	t.Helper()
	r, err := NewReveniumGroq(&Config{
		GroqAPIKey: "gsk-test",
		BaseURL:    groqURL,
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
	provider := newGroqStub(t)
	rules := testutil.NewMockRulesServer()
	t.Cleanup(rules.Close)
	metering := testutil.NewMockMeteringServer()
	t.Cleanup(metering.Close)
	rules.SetPayload("team-a", `{"rules": [{"ruleId": 7, "name": "per-dev", "metricType": "TOTAL_COST", "threshold": 50,
		"currentValue": 61, "breached": true, "shadowMode": false, "action": "BLOCK", "groupBy": "SUBSCRIBER",
		"filters": [{"dimension": "PROVIDER", "operator": "IS", "value": "Groq"}],
		"groupBreakdown": [
			{"groupValue": "alice@example.com", "currentValue": 61, "breached": true},
			{"groupValue": "bob@example.com", "currentValue": 3, "breached": false}]}]}`)

	r := newEnforcedGroq(t, provider.server.URL, rules.URL(), metering.URL(), "team-a")
	req := ChatCompletionRequest{
		Model:    "openai/gpt-oss-120b",
		Messages: []ChatMessage{{Role: "user", Content: "hi"}},
	}

	_, err := r.ChatCompletions(asSubscriber("Alice@Example.com"), req)
	var ece *enforcement.ErrCostLimitExceeded
	require.True(t, errors.As(err, &ece), "alice should be blocked, got %v", err)
	assert.Equal(t, int32(0), provider.hits.Load())

	_, err = r.ChatCompletions(asSubscriber("bob@example.com"), req)
	require.NoError(t, err)
	assert.Equal(t, int32(1), provider.hits.Load())

	_, err = r.ChatCompletions(asSubscriber("alice@example.com"), req)
	require.True(t, errors.As(err, &ece), "alice should still be blocked, got %v", err)
	assert.Equal(t, int32(1), provider.hits.Load())
}

func TestChatCompletions_EachClientEnforcesItsOwnTeamsRules(t *testing.T) {
	enforcement.Stop()
	t.Cleanup(enforcement.Stop)
	provider := newGroqStub(t)
	rules := testutil.NewMockRulesServer()
	t.Cleanup(rules.Close)
	metering := testutil.NewMockMeteringServer()
	t.Cleanup(metering.Close)
	rules.SetPayload("team-b", `{"rules": [{"ruleId": 4211, "threshold": 10, "currentValue": 12, "breached": true,
		"shadowMode": false, "action": "BLOCK", "filters": []}]}`)

	unblocked := newEnforcedGroq(t, provider.server.URL, rules.URL(), metering.URL(), "team-a")
	blockedTeam := newEnforcedGroq(t, provider.server.URL, rules.URL(), metering.URL(), "team-b")
	req := ChatCompletionRequest{
		Model:    "openai/gpt-oss-120b",
		Messages: []ChatMessage{{Role: "user", Content: "hi"}},
	}

	_, err := unblocked.ChatCompletions(asSubscriber("alice@example.com"), req)
	require.NoError(t, err)

	_, err = blockedTeam.ChatCompletions(asSubscriber("alice@example.com"), req)
	var ece *enforcement.ErrCostLimitExceeded
	require.True(t, errors.As(err, &ece), "team-b's client should be blocked, got %v", err)
	assert.Equal(t, "4211", ece.RuleID)
	assert.Equal(t, int32(1), provider.hits.Load())
}
