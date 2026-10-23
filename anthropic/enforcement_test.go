package anthropic

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	anthropicsdk "github.com/anthropics/anthropic-sdk-go"

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
		stub.hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg_1","type":"message","role":"assistant","model":"claude-haiku-4-5",` +
			`"content":[{"type":"text","text":"hi"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`))
	}))
	t.Cleanup(stub.server.Close)
	return stub
}

func newEnforcedAnthropic(t *testing.T, providerURL, rulesURL, meteringURL, teamID string) *ReveniumAnthropic {
	t.Helper()
	t.Setenv("ANTHROPIC_BASE_URL", providerURL)
	r, err := NewReveniumAnthropic(&Config{
		AnthropicAPIKey: "sk-ant-test",
		BedrockDisabled: true,
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

func messageParams(model string) anthropicsdk.MessageNewParams {
	return anthropicsdk.MessageNewParams{
		Model:     anthropicsdk.Model(model),
		MaxTokens: 16,
		Messages:  []anthropicsdk.MessageParam{anthropicsdk.NewUserMessage(anthropicsdk.NewTextBlock("hi"))},
	}
}

func TestCreateMessage_PerPersonCapBlocksOnlyThePersonOverIt(t *testing.T) {
	enforcement.Stop()
	t.Cleanup(enforcement.Stop)
	provider := newProviderStub(t)
	rules := testutil.NewMockRulesServer()
	t.Cleanup(rules.Close)
	metering := testutil.NewMockMeteringServer()
	t.Cleanup(metering.Close)
	rules.SetPayload(enforcementTestTeam, `{"rules": [{"ruleId": 7, "name": "per-dev", "metricType": "TOTAL_COST", "threshold": 50,
		"currentValue": 61, "breached": true, "shadowMode": false, "action": "BLOCK", "groupBy": "SUBSCRIBER",
		"filters": [{"dimension": "PROVIDER", "operator": "IS", "value": "Anthropic"}],
		"groupBreakdown": [
			{"groupValue": "alice@example.com", "currentValue": 61, "breached": true},
			{"groupValue": "bob@example.com", "currentValue": 3, "breached": false}]}]}`)

	messages := newEnforcedAnthropic(t, provider.server.URL, rules.URL(), metering.URL(), enforcementTestTeam).Messages()

	_, err := messages.CreateMessage(asSubscriber("Alice@Example.com"), messageParams("claude-haiku-4-5"))
	var ece *enforcement.ErrCostLimitExceeded
	require.True(t, errors.As(err, &ece), "alice should be blocked, got %v", err)
	assert.Equal(t, int32(0), provider.hits.Load())

	_, err = messages.CreateMessage(asSubscriber("bob@example.com"), messageParams("claude-haiku-4-5"))
	require.NoError(t, err)
	assert.Equal(t, int32(1), provider.hits.Load())

	_, err = messages.CreateMessageStream(asSubscriber("alice@example.com"), messageParams("claude-haiku-4-5"))
	require.True(t, errors.As(err, &ece), "alice's stream should be blocked, got %v", err)
	assert.Equal(t, int32(1), provider.hits.Load())
}
