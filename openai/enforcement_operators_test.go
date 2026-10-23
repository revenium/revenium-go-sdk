package openai

import (
	"context"
	"errors"
	"testing"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/revenium/revenium-go-sdk/core/enforcement"
	"github.com/revenium/revenium-go-sdk/core/testutil"
	"github.com/stretchr/testify/assert"
)

func withUsage(fields map[string]interface{}) context.Context {
	return core.WithUsageMetadata(context.Background(), fields)
}

func onModel(model string) enforcedCall {
	return enforcedCall{model, context.Background(), model}
}

func TestChatCompletions_EveryFilterOperatorBlocksOnlyTheCallsItCovers(t *testing.T) {
	cases := []struct {
		name    string
		filter  string
		blocked []enforcedCall
		passed  []enforcedCall
	}{
		{
			name:    "MODEL IS a dated name",
			filter:  `{"dimension": "MODEL", "operator": "IS", "value": "gpt-4.1-nano-2025-04-14"}`,
			blocked: []enforcedCall{onModel("gpt-4.1-nano"), onModel("gpt-4.1-nano-2025-04-14")},
			passed:  []enforcedCall{onModel("gpt-4o"), onModel("gpt-4.1-nano-2024-01-01")},
		},
		{
			name:    "MODEL IS_NOT an undated name",
			filter:  `{"dimension": "MODEL", "operator": "IS_NOT", "value": "gpt-4.1-nano"}`,
			blocked: []enforcedCall{onModel("gpt-4.1-nano"), onModel("gpt-4o")},
		},
		{
			name:    "MODEL IS_NOT a dated name",
			filter:  `{"dimension": "MODEL", "operator": "IS_NOT", "value": "gpt-4.1-nano-2025-04-14"}`,
			blocked: []enforcedCall{onModel("gpt-4.1-nano"), onModel("gpt-4o")},
			passed:  []enforcedCall{onModel("gpt-4.1-nano-2025-04-14")},
		},
		{
			name:    "MODEL IN",
			filter:  `{"dimension": "MODEL", "operator": "IN", "values": ["gpt-4o", "gpt-4.1-nano-2025-04-14"]}`,
			blocked: []enforcedCall{onModel("gpt-4.1-nano"), onModel("gpt-4o")},
			passed:  []enforcedCall{onModel("gpt-4o-mini")},
		},
		{
			name:    "MODEL CONTAINS",
			filter:  `{"dimension": "MODEL", "operator": "CONTAINS", "value": "nano"}`,
			blocked: []enforcedCall{onModel("gpt-4.1-nano")},
			passed:  []enforcedCall{onModel("gpt-4o")},
		},
		{
			name:    "MODEL STARTS_WITH",
			filter:  `{"dimension": "MODEL", "operator": "STARTS_WITH", "value": "gpt-4o"}`,
			blocked: []enforcedCall{onModel("gpt-4o-mini")},
			passed:  []enforcedCall{onModel("gpt-4.1-nano")},
		},
		{
			name:    "MODEL ENDS_WITH a snapshot date",
			filter:  `{"dimension": "MODEL", "operator": "ENDS_WITH", "value": "nano-2025-04-14"}`,
			blocked: []enforcedCall{onModel("gpt-4.1-nano")},
			passed:  []enforcedCall{onModel("gpt-4o")},
		},
		{
			name:    "TASK_TYPE IS",
			filter:  `{"dimension": "TASK_TYPE", "operator": "IS", "value": "summarize"}`,
			blocked: []enforcedCall{{"summarize", withUsage(map[string]interface{}{"taskType": "summarize"}), "gpt-4.1-nano"}},
			passed: []enforcedCall{{"translate", withUsage(map[string]interface{}{"taskType": "translate"}), "gpt-4.1-nano"},
				{"no task type", context.Background(), "gpt-4.1-nano"}},
		},
		{
			name:    "TASK_TYPE STARTS_WITH",
			filter:  `{"dimension": "TASK_TYPE", "operator": "STARTS_WITH", "value": "batch-"}`,
			blocked: []enforcedCall{{"batch-report", withUsage(map[string]interface{}{"taskType": "batch-report"}), "gpt-4.1-nano"}},
			passed:  []enforcedCall{{"chat", withUsage(map[string]interface{}{"taskType": "chat"}), "gpt-4.1-nano"}},
		},
		{
			name:    "CREDENTIAL IS an alias",
			filter:  `{"dimension": "CREDENTIAL", "operator": "IS", "value": "prod-key"}`,
			blocked: []enforcedCall{{"prod-key", withUsage(map[string]interface{}{"credentialAlias": "prod-key"}), "gpt-4.1-nano"}},
			passed:  []enforcedCall{{"dev-key", withUsage(map[string]interface{}{"credentialAlias": "dev-key"}), "gpt-4.1-nano"}},
		},
		{
			name:   "CREDENTIAL ENDS_WITH on a subscriber credential name",
			filter: `{"dimension": "CREDENTIAL", "operator": "ENDS_WITH", "value": "-prod"}`,
			blocked: []enforcedCall{{"billing-prod", withUsage(map[string]interface{}{
				"subscriber": map[string]interface{}{"id": "sub_1", "credential": map[string]interface{}{"name": "billing-prod"}}}), "gpt-4.1-nano"}},
			passed: []enforcedCall{{"billing-dev", withUsage(map[string]interface{}{
				"subscriber": map[string]interface{}{"id": "sub_1", "credential": map[string]interface{}{"name": "billing-dev"}}}), "gpt-4.1-nano"}},
		},
		{
			name:    "ORGANIZATION CONTAINS",
			filter:  `{"dimension": "ORGANIZATION", "operator": "CONTAINS", "value": "acme"}`,
			blocked: []enforcedCall{{"acme-eu", withUsage(map[string]interface{}{"organizationName": "acme-eu"}), "gpt-4.1-nano"}},
			passed:  []enforcedCall{{"globex", withUsage(map[string]interface{}{"organizationName": "globex"}), "gpt-4.1-nano"}},
		},
		{
			name:    "SUBSCRIBER ENDS_WITH a domain",
			filter:  `{"dimension": "SUBSCRIBER", "operator": "ENDS_WITH", "value": "@contractor.example"}`,
			blocked: []enforcedCall{{"contractor", asSubscriber("Dana@Contractor.example"), "gpt-4.1-nano"}},
			passed:  []enforcedCall{{"employee", asSubscriber("alice@example.com"), "gpt-4.1-nano"}},
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
			rules.SetPayload(enforcementTestTeam, breachedRule(`"filters": [`+tc.filter+`]`))

			completions := newEnforcedOpenAI(t, provider.server.URL, rules.URL(), metering.URL(), enforcementTestTeam).Chat().Completions()

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
