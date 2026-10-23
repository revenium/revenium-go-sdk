package openai

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	openaisdk "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"

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
		_, _ = w.Write([]byte(`{"id":"chatcmpl-1","object":"chat.completion","created":1700000000,"model":"gpt-4.1-nano",` +
			`"choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}],` +
			`"usage":{"prompt_tokens":3,"completion_tokens":1,"total_tokens":4}}`))
	}))
	t.Cleanup(stub.server.Close)
	return stub
}

type enforcedCall struct {
	name  string
	ctx   context.Context
	model string
}

func newEnforcedOpenAI(t *testing.T, providerURL, rulesURL, meteringURL, teamID string) *ReveniumOpenAI {
	t.Helper()
	r, err := NewReveniumOpenAI(&Config{
		OpenAIAPIKey: "sk-test",
		BaseURL:      providerURL,
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

func chatParams(model string) openaisdk.ChatCompletionNewParams {
	return openaisdk.ChatCompletionNewParams{
		Model:    model,
		Messages: []openaisdk.ChatCompletionMessageParamUnion{openaisdk.UserMessage("hi")},
	}
}

func asSubscriber(email string) context.Context {
	return core.WithSubscriber(context.Background(), &core.Subscriber{Email: email})
}

func asAgent(email, agent string) context.Context {
	return core.WithUsageMetadata(asSubscriber(email), map[string]interface{}{"agent": agent})
}

func breachedRule(fields string) string {
	return fmt.Sprintf(`{"rules": [{"ruleId": 7, "name": "cap", "threshold": 50, "currentValue": 61, "periodType": "MONTHLY",
		"breached": true, "shadowMode": false, "action": "BLOCK", %s}]}`, fields)
}

func TestChatCompletions_EnforcesEachRuleForTheCallingPerson(t *testing.T) {
	const providerOpenAI = `{"dimension": "PROVIDER", "operator": "IS", "value": "OPENAI"}`
	perPersonBreakdown := `"metricType": "TOTAL_COST", "groupBy": "SUBSCRIBER", "groupBreakdown": [
		{"groupValue": "alice@example.com", "currentValue": 61, "breached": true},
		{"groupValue": "bob@example.com", "currentValue": 3, "breached": false}]`

	cases := []struct {
		name    string
		payload string
		blocked []enforcedCall
		passed  []enforcedCall
	}{
		{
			name:    "team-wide cap with no filters",
			payload: breachedRule(`"filters": []`),
			blocked: []enforcedCall{{"alice", asSubscriber("alice@example.com"), "gpt-4.1-nano"}, {"bob", asSubscriber("bob@example.com"), "gpt-4.1-nano"}},
		},
		{
			name:    "per-person cap with no filters",
			payload: breachedRule(`"filters": [], ` + perPersonBreakdown),
			blocked: []enforcedCall{{"alice", asSubscriber("alice@example.com"), "gpt-4.1-nano"}},
			passed:  []enforcedCall{{"bob", asSubscriber("bob@example.com"), "gpt-4.1-nano"}},
		},
		{
			name:    "per-person cap on OpenAI",
			payload: breachedRule(`"filters": [` + providerOpenAI + `], ` + perPersonBreakdown),
			blocked: []enforcedCall{{"alice", asSubscriber("Alice@Example.com"), "gpt-4.1-nano"}},
			passed:  []enforcedCall{{"bob", asSubscriber("bob@example.com"), "gpt-4.1-nano"}},
		},
		{
			name: "department budget on OpenAI",
			payload: `{"rules": [{"ruleId": 12, "name": "dept", "threshold": 500, "currentValue": 512, "breached": true,
				"shadowMode": false, "action": "BLOCK", "groupBy": "DEPARTMENT", "departmentId": 173, "filters": [` + providerOpenAI + `]}],
				"departmentBudgetBlocks": {"alice@example.com": 12}}`,
			blocked: []enforcedCall{{"alice", asSubscriber("alice@example.com"), "gpt-4.1-nano"}},
			passed:  []enforcedCall{{"carol outside the department", asSubscriber("carol@example.com"), "gpt-4.1-nano"}},
		},
		{
			name:    "rule on a person's email",
			payload: breachedRule(`"filters": [{"dimension": "SUBSCRIBER", "operator": "IS", "value": "alice@example.com"}]`),
			blocked: []enforcedCall{
				{"alice with id and email", core.WithSubscriber(context.Background(), &core.Subscriber{ID: "sub_123", Email: "alice@example.com"}), "gpt-4.1-nano"},
				{"alice in another letter case", asSubscriber("Alice@Example.com"), "gpt-4.1-nano"},
			},
			passed: []enforcedCall{{"bob", asSubscriber("bob@example.com"), "gpt-4.1-nano"}},
		},
		{
			name:    "agent-scoped cap",
			payload: breachedRule(`"filters": [` + providerOpenAI + `, {"dimension": "AGENT", "operator": "IS", "value": "batch-bot"}]`),
			blocked: []enforcedCall{{"batch-bot", asAgent("alice@example.com", "batch-bot"), "gpt-4.1-nano"}},
			passed:  []enforcedCall{{"other-bot", asAgent("alice@example.com", "other-bot"), "gpt-4.1-nano"}},
		},
		{
			name: "cap on a list of people",
			payload: breachedRule(`"filters": [` + providerOpenAI + `, {"dimension": "SUBSCRIBER", "operator": "IN",
				"value": "alice@example.com,carol@example.com", "values": ["alice@example.com", "carol@example.com"]}]`),
			blocked: []enforcedCall{{"alice", asSubscriber("alice@example.com"), "gpt-4.1-nano"}},
			passed:  []enforcedCall{{"bob", asSubscriber("bob@example.com"), "gpt-4.1-nano"}},
		},
		{
			name:    "cap on a list of one person, alone",
			payload: breachedRule(`"filters": [{"dimension": "SUBSCRIBER", "operator": "IN", "value": "alice@example.com", "values": ["alice@example.com"]}]`),
			blocked: []enforcedCall{{"alice", asSubscriber("alice@example.com"), "gpt-4.1-nano"}},
		},
		{
			name:    "cap on every model but one snapshot",
			payload: breachedRule(`"filters": [` + providerOpenAI + `, {"dimension": "MODEL", "operator": "IS_NOT", "value": "gpt-4.1-nano-2025-04-14"}]`),
			blocked: []enforcedCall{{"gpt-4o", asSubscriber("bob@example.com"), "gpt-4o"}},
			passed:  []enforcedCall{{"gpt-4.1-nano-2025-04-14", asSubscriber("bob@example.com"), "gpt-4.1-nano-2025-04-14"}},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			enforcement.Stop()
			t.Cleanup(enforcement.Stop)
			provider := newProviderStub(t)
			rules := testutil.NewMockRulesServer()
			t.Cleanup(rules.Close)
			metering := testutil.NewMockMeteringServer()
			t.Cleanup(metering.Close)
			rules.SetPayload(enforcementTestTeam, tc.payload)

			client := newEnforcedOpenAI(t, provider.server.URL, rules.URL(), metering.URL(), enforcementTestTeam)
			completions := client.Chat().Completions()

			for _, call := range tc.blocked {
				before := provider.hits.Load()
				_, err := completions.New(call.ctx, chatParams(call.model))
				var ece *enforcement.ErrCostLimitExceeded
				assert.True(t, errors.As(err, &ece), "%s should be blocked, got %v", call.name, err)
				assert.Equal(t, before, provider.hits.Load(), "%s reached the provider", call.name)
			}
			for _, call := range tc.passed {
				before := provider.hits.Load()
				_, err := completions.New(call.ctx, chatParams(call.model))
				assert.NoError(t, err, "%s should pass", call.name)
				assert.Equal(t, before+1, provider.hits.Load(), "%s did not reach the provider", call.name)
			}
		})
	}
}

func TestChatCompletionsStreaming_PerPersonCapBlocksOnlyThePersonOverIt(t *testing.T) {
	enforcement.Stop()
	t.Cleanup(enforcement.Stop)
	providerServer := httptest.NewServer(sseChatHandler(t))
	t.Cleanup(providerServer.Close)
	rules := testutil.NewMockRulesServer()
	t.Cleanup(rules.Close)
	metering := testutil.NewMockMeteringServer()
	t.Cleanup(metering.Close)
	rules.SetPayload(enforcementTestTeam, breachedRule(`"metricType": "TOTAL_COST", "groupBy": "SUBSCRIBER",
		"filters": [{"dimension": "PROVIDER", "operator": "IS", "value": "OPENAI"}],
		"groupBreakdown": [{"groupValue": "alice@example.com", "currentValue": 61, "breached": true}]`))

	client := newEnforcedOpenAI(t, providerServer.URL, rules.URL(), metering.URL(), enforcementTestTeam)
	completions := client.Chat().Completions()

	_, err := completions.NewStreaming(asSubscriber("alice@example.com"), chatParams("gpt-4.1-nano"))
	var ece *enforcement.ErrCostLimitExceeded
	require.True(t, errors.As(err, &ece))
	assert.Equal(t, 61.0, ece.CurrentValue)

	stream, err := completions.NewStreaming(asSubscriber("bob@example.com"), chatParams("gpt-4.1-nano"))
	require.NoError(t, err)
	for stream.Next() {
	}
	require.NoError(t, stream.Err())
	require.NoError(t, stream.Close())
}

type enforcedSurface struct {
	name string
	call func(ctx context.Context, r *ReveniumOpenAI) error
}

func responsesParams() responses.ResponseNewParams {
	return responses.ResponseNewParams{
		Model: "gpt-4.1-nano",
		Input: responses.ResponseNewParamsInputUnion{OfString: openaisdk.String("hi")},
	}
}

func enforcedSurfaces() []enforcedSurface {
	surfaces := []enforcedSurface{
		{"responses create", func(ctx context.Context, r *ReveniumOpenAI) error {
			_, err := r.Responses().Create(ctx, responsesParams())
			return err
		}},
		{"responses streaming", func(ctx context.Context, r *ReveniumOpenAI) error {
			stream, err := r.Responses().CreateStreaming(ctx, responsesParams())
			if stream != nil {
				_ = stream.Close()
			}
			return err
		}},
		{"embeddings", func(ctx context.Context, r *ReveniumOpenAI) error {
			_, err := r.Embeddings().Create(ctx, openaisdk.EmbeddingNewParams{
				Model: "text-embedding-3-small",
				Input: openaisdk.EmbeddingNewParamsInputUnion{OfString: openaisdk.String("hi")},
			})
			return err
		}},
	}
	for _, path := range multimodalPaths() {
		surfaces = append(surfaces, enforcedSurface{path.name, path.call})
	}
	return surfaces
}

func nonChatOpenAIHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/responses"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"resp_1","object":"response","created_at":1700000000,"status":"completed",` +
				`"model":"gpt-4.1-nano","output":[],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`))
		case strings.HasSuffix(r.URL.Path, "/embeddings"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"object":"list","data":[{"object":"embedding","index":0,"embedding":[0.1]}],` +
				`"model":"text-embedding-3-small","usage":{"prompt_tokens":1,"total_tokens":1}}`))
		default:
			multimodalOpenAIHandler().ServeHTTP(w, r)
		}
	})
}

func TestNonChatSurfaces_BreachedBlockRuleStopsTheCallBeforeTheProvider(t *testing.T) {
	for _, surface := range enforcedSurfaces() {
		t.Run(surface.name, func(t *testing.T) {
			enforcement.Stop()
			t.Cleanup(enforcement.Stop)
			var hits atomic.Int32
			providerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				hits.Add(1)
				nonChatOpenAIHandler().ServeHTTP(w, r)
			}))
			t.Cleanup(providerServer.Close)
			rules := testutil.NewMockRulesServer()
			t.Cleanup(rules.Close)
			metering := testutil.NewMockMeteringServer()
			t.Cleanup(metering.Close)
			rules.SetPayload(enforcementTestTeam, breachedRule(`"filters": [{"dimension": "SUBSCRIBER", "operator": "IS", "value": "alice@example.com"}]`))

			client := newEnforcedOpenAI(t, providerServer.URL, rules.URL(), metering.URL(), enforcementTestTeam)

			err := surface.call(asSubscriber("alice@example.com"), client)
			var ece *enforcement.ErrCostLimitExceeded
			require.True(t, errors.As(err, &ece), "alice should be blocked, got %v", err)
			assert.Equal(t, int32(0), hits.Load(), "alice's call reached the provider")

			require.NoError(t, surface.call(asSubscriber("bob@example.com"), client))
			assert.Equal(t, int32(1), hits.Load(), "bob's call did not reach the provider")
		})
	}
}
