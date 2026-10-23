package perplexity

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	openai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/shared/constant"

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
		_, _ = w.Write([]byte(`{"id":"pplx-1","object":"chat.completion","created":1700000000,"model":"sonar",` +
			`"choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}],` +
			`"usage":{"prompt_tokens":3,"completion_tokens":1,"total_tokens":4}}`))
	}))
	t.Cleanup(stub.server.Close)
	return stub
}

func newEnforcedPerplexity(t *testing.T, providerURL, rulesURL, meteringURL, teamID string) *ReveniumPerplexity {
	t.Helper()
	r, err := NewReveniumPerplexity(&Config{
		PerplexityAPIKey:  "pplx-test",
		PerplexityBaseURL: providerURL,
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

func chatParams(model string) openai.ChatCompletionNewParams {
	return openai.ChatCompletionNewParams{
		Model: model,
		Messages: []openai.ChatCompletionMessageParamUnion{
			{OfUser: &openai.ChatCompletionUserMessageParam{
				Role:    constant.ValueOf[constant.User](),
				Content: openai.ChatCompletionUserMessageParamContentUnion{OfString: openai.String("hi")},
			}},
		},
	}
}

func asEnforcedSubscriber(email string) context.Context {
	return core.WithSubscriber(context.Background(), &core.Subscriber{Email: email})
}

func TestCompletions_PerPersonCapBlocksOnlyThePersonOverIt(t *testing.T) {
	enforcement.Stop()
	t.Cleanup(enforcement.Stop)
	provider := newProviderStub(t)
	rules := testutil.NewMockRulesServer()
	t.Cleanup(rules.Close)
	metering := testutil.NewMockMeteringServer()
	t.Cleanup(metering.Close)
	rules.SetPayload(enforcementTestTeam, `{"rules": [{"ruleId": 7, "name": "per-dev", "metricType": "TOTAL_COST", "threshold": 50,
		"currentValue": 61, "breached": true, "shadowMode": false, "action": "BLOCK", "groupBy": "SUBSCRIBER",
		"filters": [{"dimension": "PROVIDER", "operator": "IS", "value": "PERPLEXITY"}],
		"groupBreakdown": [
			{"groupValue": "alice@example.com", "currentValue": 61, "breached": true},
			{"groupValue": "bob@example.com", "currentValue": 3, "breached": false}]}]}`)

	client := newEnforcedPerplexity(t, provider.server.URL, rules.URL(), metering.URL(), enforcementTestTeam)
	completions := client.Chat().Completions()

	_, err := completions.New(asEnforcedSubscriber("Alice@Example.com"), chatParams("sonar"))
	var ece *enforcement.ErrCostLimitExceeded
	require.True(t, errors.As(err, &ece), "alice should be blocked, got %v", err)
	assert.Equal(t, int32(0), provider.hits.Load())

	_, err = completions.New(asEnforcedSubscriber("bob@example.com"), chatParams("sonar"))
	require.NoError(t, err)
	assert.Equal(t, int32(1), provider.hits.Load())

	_, err = completions.NewStreaming(asEnforcedSubscriber("alice@example.com"), chatParams("sonar"))
	require.True(t, errors.As(err, &ece), "alice's stream should be blocked, got %v", err)
	assert.Equal(t, int32(1), provider.hits.Load())
}
