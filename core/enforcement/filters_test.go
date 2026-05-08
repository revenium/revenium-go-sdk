package enforcement

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRuleUnmarshal_DerivesFlatScopeFromModelFilter(t *testing.T) {
	// Mirrors the BE payload pasted in BACK-1340: scope arrives as a
	// `filters[]` array with no top-level `model` field.
	body := []byte(`{
		"ruleId": 5555,
		"threshold": 500,
		"currentValue": 4338,
		"breached": true,
		"filters": [{"dimension": "MODEL", "operator": "IS", "value": "llama3.1:8b"}],
		"shadowMode": false,
		"action": "BLOCK"
	}`)

	var r Rule
	require.NoError(t, json.Unmarshal(body, &r))
	assert.Equal(t, "llama3.1:8b", r.Model, "MODEL filter should populate r.Model")
	assert.Equal(t, "", r.SubscriberID)
	assert.Equal(t, "", r.ProductName)
	assert.Equal(t, "", r.Provider)
	require.Len(t, r.Filters, 1)
	assert.Equal(t, DimensionModel, r.Filters[0].Dimension)
	assert.Equal(t, OperatorIs, r.Filters[0].Operator)
}

func TestRuleUnmarshal_DerivesEachDimension(t *testing.T) {
	cases := []struct {
		name    string
		dim     Dimension
		value   string
		field   func(r *Rule) string
		wantSet string
	}{
		{"subscriber", DimensionSubscriber, "sub-42", func(r *Rule) string { return r.SubscriberID }, "sub-42"},
		{"product", DimensionProduct, "prod-x", func(r *Rule) string { return r.ProductName }, "prod-x"},
		{"model", DimensionModel, "gpt-4o-mini", func(r *Rule) string { return r.Model }, "gpt-4o-mini"},
		{"provider", DimensionProvider, "OPENAI", func(r *Rule) string { return r.Provider }, "OPENAI"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body, err := json.Marshal(map[string]any{
				"ruleId":     1,
				"threshold":  100.0,
				"action":     "BLOCK",
				"breached":   true,
				"shadowMode": false,
				"filters": []map[string]string{
					{"dimension": string(tc.dim), "operator": "IS", "value": tc.value},
				},
			})
			require.NoError(t, err)

			var r Rule
			require.NoError(t, json.Unmarshal(body, &r))
			assert.Equal(t, tc.wantSet, tc.field(&r))
		})
	}
}

func TestRuleUnmarshal_NonIsOperatorDoesNotPopulateFlatField(t *testing.T) {
	// Equality-based matches() can't honour CONTAINS/STARTS_WITH/etc., so
	// derivation must skip those operators rather than silently turning
	// "model contains 4o" into "model = 4o".
	body := []byte(`{
		"ruleId": 1, "threshold": 1, "action": "BLOCK", "breached": true, "shadowMode": false,
		"filters": [{"dimension": "MODEL", "operator": "CONTAINS", "value": "4o"}]
	}`)
	var r Rule
	require.NoError(t, json.Unmarshal(body, &r))
	assert.Equal(t, "", r.Model, "CONTAINS must not populate Model — evaluator only supports equality")
	require.Len(t, r.Filters, 1, "filter is preserved on the wire shape even when not derived")
}

func TestRuleUnmarshal_UnknownDimensionPreservedButNoDerivation(t *testing.T) {
	// ORGANIZATION/CREDENTIAL/AGENT/TASK_TYPE exist in the server enum but
	// have no flat-field counterpart. The filter should round-trip; the
	// rule then falls into matches()'s no-scope guard and is skipped.
	body := []byte(`{
		"ruleId": 1, "threshold": 1, "action": "BLOCK", "breached": true, "shadowMode": false,
		"filters": [{"dimension": "CREDENTIAL", "operator": "IS", "value": "cred-1"}]
	}`)
	var r Rule
	require.NoError(t, json.Unmarshal(body, &r))
	assert.Equal(t, "", r.Model)
	assert.Equal(t, "", r.SubscriberID)
	require.Len(t, r.Filters, 1)
	assert.Equal(t, DimensionCredential, r.Filters[0].Dimension)
}

func TestRuleUnmarshal_FlatFieldWinsWhenBothPresent(t *testing.T) {
	// If a server ever emits both shapes (transitional payloads), we want
	// the flat field to win — that's the legacy shape the SDK shipped
	// against, and clobbering it would be a behaviour change.
	body := []byte(`{
		"ruleId": 1, "threshold": 1, "action": "BLOCK", "breached": true, "shadowMode": false,
		"model": "explicit-model",
		"filters": [{"dimension": "MODEL", "operator": "IS", "value": "filter-model"}]
	}`)
	var r Rule
	require.NoError(t, json.Unmarshal(body, &r))
	assert.Equal(t, "explicit-model", r.Model)
}

func TestRuleUnmarshal_NoFiltersBackCompat(t *testing.T) {
	body := []byte(`{
		"ruleId": 7, "name": "legacy", "threshold": 100, "currentValue": 50,
		"action": "BLOCK", "breached": true, "shadowMode": false,
		"subscriberId": "sub1", "model": "gpt-4o-mini"
	}`)
	var r Rule
	require.NoError(t, json.Unmarshal(body, &r))
	assert.Equal(t, "sub1", r.SubscriberID)
	assert.Equal(t, "gpt-4o-mini", r.Model)
	assert.Empty(t, r.Filters)
}

func TestRuleUnmarshal_MultipleFiltersComposeAcrossDimensions(t *testing.T) {
	body := []byte(`{
		"ruleId": 9, "threshold": 1, "action": "BLOCK", "breached": true, "shadowMode": false,
		"filters": [
			{"dimension": "MODEL", "operator": "IS", "value": "gpt-4o-mini"},
			{"dimension": "PROVIDER", "operator": "IS", "value": "OPENAI"}
		]
	}`)
	var r Rule
	require.NoError(t, json.Unmarshal(body, &r))
	assert.Equal(t, "gpt-4o-mini", r.Model)
	assert.Equal(t, "OPENAI", r.Provider)
}

func TestRuleUnmarshal_RoundTripPreservesFiltersAndDerives(t *testing.T) {
	original := []byte(`{
		"ruleId": 5555,
		"name": "monthly-cap",
		"threshold": 500,
		"currentValue": 4338,
		"action": "BLOCK",
		"breached": true,
		"shadowMode": false,
		"filters": [{"dimension": "MODEL", "operator": "IS", "value": "llama3.1:8b"}]
	}`)

	var r Rule
	require.NoError(t, json.Unmarshal(original, &r))

	encoded, err := json.Marshal(r)
	require.NoError(t, err)

	var r2 Rule
	require.NoError(t, json.Unmarshal(encoded, &r2))
	assert.Equal(t, r.RuleID, r2.RuleID)
	assert.Equal(t, r.Model, r2.Model, "Model must survive round-trip — populated on both decodes")
	require.Len(t, r2.Filters, 1)
	assert.Equal(t, "llama3.1:8b", r2.Filters[0].Value)
}

func TestEvaluate_FilterScopedRuleEnforcesAfterDerivation(t *testing.T) {
	// End-to-end: BE-shaped payload feeds into evaluate() and returns
	// ErrCostLimitExceeded when the request matches. This is the exact
	// pre-call behaviour BACK-1340 reports as broken in v2.12.0.
	body := []byte(`{
		"ruleId": 5555,
		"threshold": 500,
		"currentValue": 4338,
		"breached": true,
		"filters": [{"dimension": "MODEL", "operator": "IS", "value": "llama3.1:8b"}],
		"shadowMode": false,
		"action": "BLOCK"
	}`)
	var r Rule
	require.NoError(t, json.Unmarshal(body, &r))

	err := evaluate([]Rule{r}, EvalContext{Model: "llama3.1:8b"})
	require.Error(t, err)
	var ece *ErrCostLimitExceeded
	require.True(t, errors.As(err, &ece))
	assert.Equal(t, "5555", ece.RuleID)

	// Mismatched model on the same rule must allow.
	require.NoError(t, evaluate([]Rule{r}, EvalContext{Model: "gpt-4o-mini"}))
}

func TestEvaluate_FilterScopedRuleSkipsWhenOnlyUnknownDimensions(t *testing.T) {
	// A rule whose only filter is a dimension the SDK can't match (e.g.
	// CREDENTIAL — no matching field on EvalContext) must NOT silently
	// match every request. The no-scope guard in matches() is what keeps
	// this safe; this test pins that behaviour.
	body := []byte(`{
		"ruleId": 1, "threshold": 1, "action": "BLOCK", "breached": true, "shadowMode": false,
		"filters": [{"dimension": "CREDENTIAL", "operator": "IS", "value": "cred-1"}]
	}`)
	var r Rule
	require.NoError(t, json.Unmarshal(body, &r))
	require.NoError(t, evaluate([]Rule{r}, EvalContext{Model: "anything"}))
}
