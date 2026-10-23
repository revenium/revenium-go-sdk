package enforcement

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRuleUnmarshal_KeepsWireScopeAndGrouping(t *testing.T) {
	body := []byte(`{
		"ruleId": 5555, "name": "per-dev", "metricType": "TOTAL_COST",
		"threshold": 50, "currentValue": 61, "breached": true,
		"shadowMode": false, "action": "BLOCK",
		"groupBy": "SUBSCRIBER",
		"groupBreakdown": [
			{"groupValue": "alice@example.com", "displayName": "alice@example.com", "currentValue": 61, "usagePercent": 1.22, "breached": true}
		],
		"filters": [
			{"dimension": "PROVIDER", "operator": "IS", "value": "OPENAI", "values": []},
			{"dimension": "SUBSCRIBER", "operator": "IN", "value": "alice,carol", "values": ["alice", "carol"]}
		]
	}`)

	var r Rule
	require.NoError(t, json.Unmarshal(body, &r))
	assert.Equal(t, Dimension("SUBSCRIBER"), r.GroupBy)
	assert.Equal(t, "TOTAL_COST", r.MetricType)
	require.Len(t, r.GroupBreakdown, 1)
	assert.Equal(t, GroupEntry{GroupValue: "alice@example.com", CurrentValue: 61, Breached: true}, r.GroupBreakdown[0])
	require.Len(t, r.Filters, 2)
	assert.Equal(t, []string{"alice", "carol"}, r.Filters[1].Values)
}

func TestRuleUnmarshal_DepartmentIDUnderEitherSpelling(t *testing.T) {
	for _, body := range []string{
		`{"ruleId": 1, "departmentId": 173}`,
		`{"ruleId": 1, "orgUnitId": 173}`,
	} {
		var r Rule
		require.NoError(t, json.Unmarshal([]byte(body), &r))
		require.NotNil(t, r.DepartmentID, body)
		assert.Equal(t, int64(173), *r.DepartmentID)
		assert.True(t, r.isDepartmentRule())
	}
}

func TestRuleUnmarshal_RoundTripPreservesScope(t *testing.T) {
	original := []byte(`{
		"ruleId": 5555, "threshold": 500, "currentValue": 4338,
		"action": "BLOCK", "breached": true, "shadowMode": false,
		"filters": [{"dimension": "MODEL", "operator": "IS_NOT", "value": "llama3.1:8b"}]
	}`)
	var r Rule
	require.NoError(t, json.Unmarshal(original, &r))

	encoded, err := json.Marshal(r)
	require.NoError(t, err)

	var r2 Rule
	require.NoError(t, json.Unmarshal(encoded, &r2))
	assert.Equal(t, r.RuleID, r2.RuleID)
	require.Len(t, r2.Filters, 1)
	assert.Equal(t, OperatorIsNot, r2.Filters[0].Operator)
	assert.Equal(t, "llama3.1:8b", r2.Filters[0].Value)
}

func TestFilterUnmarshal_NullValueConstrainsNothingButEmptyStringDoes(t *testing.T) {
	var unset, empty Filter
	require.NoError(t, json.Unmarshal([]byte(`{"dimension": "AGENT", "operator": "IS", "value": null}`), &unset))
	require.NoError(t, json.Unmarshal([]byte(`{"dimension": "AGENT", "operator": "IS", "value": ""}`), &empty))

	caller := EvalContext{Agent: "batch-bot"}
	assert.True(t, unset.matches(caller))
	assert.False(t, empty.matches(caller))
}

// TestFilterMatches pins the platform's EnforcementFilterMatcher semantics.
func TestFilterMatches(t *testing.T) {
	idOnly := EvalContext{SubscriberID: "Sub_123"}
	full := EvalContext{
		Model: "gpt-4.1-nano", Provider: "OPENAI", ProductName: "assistant", Agent: "batch-bot",
		Organization: "acme", TaskType: "summarize", Credential: "prod-key",
	}

	cases := []struct {
		name   string
		filter Filter
		caller EvalContext
		want   bool
	}{
		{"subscriber IS email, caller sends id and email", isFilter(DimensionSubscriber, "alice@example.com"), aliceWithID, true},
		{"subscriber IS email, caller email differs in case", isFilter(DimensionSubscriber, "alice@example.com"), aliceUpper, true},
		{"subscriber IS email padded and upper case in rule", isFilter(DimensionSubscriber, " ALICE@example.com "), aliceWithID, true},
		{"subscriber IS id, caller also sends email", isFilter(DimensionSubscriber, "sub_123"), aliceWithID, false},
		{"subscriber IS id, caller sends id only, exact", isFilter(DimensionSubscriber, "Sub_123"), idOnly, true},
		{"subscriber IS id, caller id differs in case", isFilter(DimensionSubscriber, "sub_123"), idOnly, false},
		{"subscriber IS_NOT email, same person other case", op(DimensionSubscriber, OperatorIsNot, "ALICE@example.com"), aliceWithID, false},
		{"subscriber IS_NOT, anonymous caller", op(DimensionSubscriber, OperatorIsNot, "alice@example.com"), EvalContext{}, true},
		{"subscriber IS, anonymous caller", isFilter(DimensionSubscriber, "alice@example.com"), EvalContext{}, false},
		{"subscriber CONTAINS ignores email case", op(DimensionSubscriber, OperatorContains, "EXAMPLE.COM"), aliceUpper, true},
		{"subscriber ENDS_WITH on email", op(DimensionSubscriber, OperatorEndsWith, "@example.com"), aliceWithID, true},
		{"subscriber IN email list, case-insensitive", in(DimensionSubscriber, "carol@example.com", "ALICE@example.com"), aliceWithID, true},
		{"subscriber IN, caller not listed", in(DimensionSubscriber, "alice@example.com", "carol@example.com"), EvalContext{SubscriberEmail: "bob@example.com"}, false},
		{"subscriber IN id list, exact", in(DimensionSubscriber, "sub_123"), idOnly, false},
		{"IN with no values constrains nothing", in(DimensionModel), full, true},
		{"model IS", isFilter(DimensionModel, "gpt-4.1-nano"), full, true},
		{"model IS other", isFilter(DimensionModel, "gpt-4o"), full, false},
		{"model IS_NOT same undated model, which may be metered under any date", op(DimensionModel, OperatorIsNot, "gpt-4.1-nano"), full.ServedModelMayBeDated(), true},
		{"model IS_NOT same model, recorded under exactly that name", op(DimensionModel, OperatorIsNot, "gpt-4.1-nano"), full, false},
		{"model IS_NOT same dated model", op(DimensionModel, OperatorIsNot, "gpt-4.1-nano-2025-04-14"), EvalContext{Model: "gpt-4.1-nano-2025-04-14"}, false},
		{"model IS_NOT other model", op(DimensionModel, OperatorIsNot, "gpt-4o"), full, true},
		{"model CONTAINS", op(DimensionModel, OperatorContains, "4.1"), full, true},
		{"model STARTS_WITH", op(DimensionModel, OperatorStartsWith, "gpt-"), full, true},
		{"model ENDS_WITH miss", op(DimensionModel, OperatorEndsWith, "mini"), full, false},
		{"model comparison is case-sensitive", isFilter(DimensionModel, "GPT-4.1-nano"), full, false},
		{"model IN", in(DimensionModel, "gpt-4o", "gpt-4.1-nano"), full, true},
		{"unknown operator never matches", op(DimensionModel, Operator("MATCHES"), "gpt"), full, false},
		{"agent IS", isFilter(DimensionAgent, "batch-bot"), full, true},
		{"agent IS other agent", isFilter(DimensionAgent, "batch-bot"), EvalContext{Agent: "other-bot"}, false},
		{"agent IS, no agent sent", isFilter(DimensionAgent, "batch-bot"), EvalContext{}, false},
		{"agent IS_NOT, no agent sent", op(DimensionAgent, OperatorIsNot, "batch-bot"), EvalContext{}, true},
		{"product IS", isFilter(DimensionProduct, "assistant"), full, true},
		{"organization IS", isFilter(DimensionOrganization, "acme"), full, true},
		{"organization IN miss", in(DimensionOrganization, "globex"), full, false},
		{"task type IS", isFilter(DimensionTaskType, "summarize"), full, true},
		{"credential IS", isFilter(DimensionCredential, "prod-key"), full, true},
		{"credential IS other", isFilter(DimensionCredential, "dev-key"), full, false},
		{"provider IS exact", isFilter(DimensionProvider, "OPENAI"), full, true},
		{"provider IS ignores case", isFilter(DimensionProvider, "openai"), full, true},
		{"provider IS ANTHROPIC covers Foundry traffic", isFilter(DimensionProvider, "ANTHROPIC"), EvalContext{Provider: "Foundry"}, true},
		{"provider IS_NOT ANTHROPIC excludes Foundry traffic", op(DimensionProvider, OperatorIsNot, "ANTHROPIC"), EvalContext{Provider: "Foundry"}, false},
		{"provider IN ANTHROPIC covers Foundry traffic", in(DimensionProvider, "ANTHROPIC"), EvalContext{Provider: "Foundry"}, true},
		{"provider IS FOUNDRY does not cover Anthropic", isFilter(DimensionProvider, "FOUNDRY"), EvalContext{Provider: "ANTHROPIC"}, false},
		{"department filter never matches on the client", isFilter(DimensionDepartment, "173"), full, false},
		{"unknown dimension never matches", isFilter(Dimension("WORKSPACE"), "w1"), full, false},
		{"unknown dimension without a value never matches", Filter{Dimension: Dimension("WORKSPACE"), Operator: OperatorIs}, full, false},
		{"department IN with no values never matches", in(DimensionDepartment), full, false},
		{"unknown operator without a value never matches", Filter{Dimension: DimensionModel, Operator: Operator("MATCHES")}, full, false},
		{"missing operator never matches", Filter{Dimension: DimensionModel, Value: "gpt-4.1-nano"}, full, false},
		{"known dimension and operator without a value constrains nothing", Filter{Dimension: DimensionAgent, Operator: OperatorIs}, full, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.filter.matches(tc.caller))
		})
	}
}

func TestCovers_FiltersComposeWithAnd(t *testing.T) {
	r := Rule{Filters: []Filter{
		isFilter(DimensionProvider, "OPENAI"),
		op(DimensionModel, OperatorIsNot, "gpt-4.1-nano-2025-04-14"),
	}}
	assert.True(t, r.covers(EvalContext{Provider: "OPENAI", Model: "gpt-4o"}))
	assert.False(t, r.covers(EvalContext{Provider: "OPENAI", Model: "gpt-4.1-nano-2025-04-14"}))
	assert.False(t, r.covers(EvalContext{Provider: "AZURE", Model: "gpt-4o"}))
}

func TestEvaluate_FilterScopedRuleEnforcesFromWirePayload(t *testing.T) {
	body := []byte(`{
		"ruleId": 5555, "threshold": 500, "currentValue": 4338, "breached": true,
		"filters": [{"dimension": "MODEL", "operator": "IS", "value": "llama3.1:8b"}],
		"shadowMode": false, "action": "BLOCK"
	}`)
	var r Rule
	require.NoError(t, json.Unmarshal(body, &r))

	err := evaluate(rulesOf(r), EvalContext{Model: "llama3.1:8b"})
	var ece *ErrCostLimitExceeded
	require.True(t, errors.As(err, &ece))
	assert.Equal(t, "5555", ece.RuleID)

	require.NoError(t, evaluate(rulesOf(r), EvalContext{Model: "gpt-4o-mini"}))
}

func isFilter(d Dimension, value string) Filter {
	return op(d, OperatorIs, value)
}

func op(d Dimension, o Operator, value string) Filter {
	return Filter{Dimension: d, Operator: o, Value: value}
}

func in(d Dimension, values ...string) Filter {
	return Filter{Dimension: d, Operator: OperatorIn, Values: values}
}
