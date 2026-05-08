package enforcement

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEvaluate_EmptyRulesAllows(t *testing.T) {
	require.NoError(t, evaluate(nil, EvalContext{SubscriberID: "sub1"}))
}

func TestEvaluate_NonBreachedIsSkipped(t *testing.T) {
	rules := []Rule{
		{
			RuleID: 1, Name: "not-breached", SubscriberID: "sub1",
			Threshold: 100, CurrentValue: 50,
			Action: ActionBlock, Breached: false,
		},
	}
	require.NoError(t, evaluate(rules, EvalContext{SubscriberID: "sub1"}))
}

func TestEvaluate_BlockRuleReturnsStructuredError(t *testing.T) {
	rules := []Rule{
		{
			RuleID: 42, Name: "monthly-cap", SubscriberID: "sub1",
			Threshold: 100, CurrentValue: 150, PeriodType: "MONTHLY",
			Action: ActionBlock, Breached: true,
		},
	}
	err := evaluate(rules, EvalContext{SubscriberID: "sub1"})
	require.Error(t, err)
	var ece *ErrCostLimitExceeded
	require.True(t, errors.As(err, &ece), "expected ErrCostLimitExceeded, got %T", err)
	assert.Equal(t, "42", ece.RuleID)
	assert.Equal(t, "monthly-cap", ece.RuleName)
	assert.Equal(t, 100.0, ece.Threshold)
	assert.Equal(t, 150.0, ece.CurrentValue)
	assert.Equal(t, "MONTHLY", ece.PeriodType)
	assert.Equal(t, ActionBlock, ece.Action)
	assert.Equal(t, "sub1", ece.Context.SubscriberID)
}

func TestEvaluate_ShadowModeNeverBlocks(t *testing.T) {
	rules := []Rule{
		{
			RuleID: 1, Name: "shadow", SubscriberID: "sub1",
			Threshold: 100, CurrentValue: 200,
			Action: ActionBlock, Breached: true, ShadowMode: true,
		},
	}
	require.NoError(t, evaluate(rules, EvalContext{SubscriberID: "sub1"}),
		"shadowMode rules must never throw — even with action=BLOCK and breached=true")
}

func TestEvaluate_WarnOnlyDoesNotBlock(t *testing.T) {
	rules := []Rule{
		{
			RuleID: 1, SubscriberID: "sub1",
			Threshold: 100, CurrentValue: 200,
			Action: ActionWarnOnly, Breached: true,
		},
	}
	require.NoError(t, evaluate(rules, EvalContext{SubscriberID: "sub1"}))
}

func TestEvaluate_ThrottleDoesNotBlock(t *testing.T) {
	rules := []Rule{
		{
			RuleID: 1, SubscriberID: "sub1",
			Threshold: 100, CurrentValue: 200,
			Action: ActionThrottle, Breached: true,
		},
	}
	require.NoError(t, evaluate(rules, EvalContext{SubscriberID: "sub1"}))
}

func TestEvaluate_NonMatchingSubscriberIsSkipped(t *testing.T) {
	rules := []Rule{
		{
			RuleID: 1, SubscriberID: "other",
			Threshold: 100, CurrentValue: 200,
			Action: ActionBlock, Breached: true,
		},
	}
	require.NoError(t, evaluate(rules, EvalContext{SubscriberID: "sub1"}))
}

func TestEvaluate_AllEmptyScopeIsSkipped(t *testing.T) {
	// A rule with every scope field empty would act as a team-wide global
	// block, which is almost always a misconfiguration. The evaluator skips
	// these rules instead of letting one bad server payload deny service
	// across every tenant.
	rules := []Rule{
		{RuleID: 1, Threshold: 100, CurrentValue: 200, Action: ActionBlock, Breached: true},
	}
	require.NoError(t, evaluate(rules, EvalContext{SubscriberID: "any-sub", Model: "any-model"}))
}

func TestEvaluate_PartialScopeMatchesAnyWithinDimension(t *testing.T) {
	rules := []Rule{
		{
			RuleID: 1, Model: "gpt-4o-mini",
			Threshold: 100, CurrentValue: 200,
			Action: ActionBlock, Breached: true,
		},
	}
	err := evaluate(rules, EvalContext{SubscriberID: "any-sub", Model: "gpt-4o-mini", Provider: "OPENAI"})
	require.Error(t, err)
	var ece *ErrCostLimitExceeded
	require.True(t, errors.As(err, &ece))
}

func TestEvaluate_FirstMatchingBlockWins(t *testing.T) {
	rules := []Rule{
		{RuleID: 1, SubscriberID: "sub1", Threshold: 100, CurrentValue: 200,
			Action: ActionBlock, Breached: true, Name: "first"},
		{RuleID: 2, SubscriberID: "sub1", Threshold: 50, CurrentValue: 150,
			Action: ActionBlock, Breached: true, Name: "second"},
	}
	err := evaluate(rules, EvalContext{SubscriberID: "sub1"})
	var ece *ErrCostLimitExceeded
	require.True(t, errors.As(err, &ece))
	assert.Equal(t, "1", ece.RuleID, "first matching rule should win")
}

func TestMatches_EmptyFieldIsWildcard(t *testing.T) {
	r := &Rule{SubscriberID: "sub1"} // ProductName/Model/Provider empty = match any
	assert.True(t, matches(r, EvalContext{SubscriberID: "sub1", ProductName: "whatever"}))
	assert.False(t, matches(r, EvalContext{SubscriberID: "other", ProductName: "whatever"}))
}
