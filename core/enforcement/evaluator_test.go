package enforcement

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEvaluate_EmptyRulesAllows(t *testing.T) {
	require.NoError(t, evaluate(ruleSet{}, EvalContext{SubscriberID: "sub1"}))
}

func TestEvaluate_NonBreachedIsSkipped(t *testing.T) {
	rules := []Rule{
		{
			RuleID: 1, Name: "not-breached", Filters: []Filter{isFilter(DimensionSubscriber, "sub1")},
			Threshold: 100, CurrentValue: 50,
			Action: ActionBlock, Breached: false,
		},
	}
	require.NoError(t, evaluate(rulesOf(rules...), EvalContext{SubscriberID: "sub1"}))
}

func TestEvaluate_BlockRuleReturnsStructuredError(t *testing.T) {
	rules := []Rule{
		{
			RuleID: 42, Name: "monthly-cap", Filters: []Filter{isFilter(DimensionSubscriber, "sub1")},
			Threshold: 100, CurrentValue: 150, PeriodType: "MONTHLY",
			Action: ActionBlock, Breached: true,
		},
	}
	err := evaluate(rulesOf(rules...), EvalContext{SubscriberID: "sub1"})
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
			RuleID: 1, Name: "shadow", Filters: []Filter{isFilter(DimensionSubscriber, "sub1")},
			Threshold: 100, CurrentValue: 200,
			Action: ActionBlock, Breached: true, ShadowMode: true,
		},
	}
	require.NoError(t, evaluate(rulesOf(rules...), EvalContext{SubscriberID: "sub1"}),
		"shadowMode rules must never throw — even with action=BLOCK and breached=true")
}

func TestEvaluate_WarnOnlyDoesNotBlock(t *testing.T) {
	rules := []Rule{
		{
			RuleID: 1, Filters: []Filter{isFilter(DimensionSubscriber, "sub1")},
			Threshold: 100, CurrentValue: 200,
			Action: ActionWarnOnly, Breached: true,
		},
	}
	require.NoError(t, evaluate(rulesOf(rules...), EvalContext{SubscriberID: "sub1"}))
}

func TestEvaluate_ThrottleDoesNotBlock(t *testing.T) {
	rules := []Rule{
		{
			RuleID: 1, Filters: []Filter{isFilter(DimensionSubscriber, "sub1")},
			Threshold: 100, CurrentValue: 200,
			Action: ActionThrottle, Breached: true,
		},
	}
	require.NoError(t, evaluate(rulesOf(rules...), EvalContext{SubscriberID: "sub1"}))
}

func TestEvaluate_ActionIsNormalizedBeforeComparison(t *testing.T) {
	ruleWith := func(action Action) ruleSet {
		return rulesOf(Rule{
			RuleID: 1, Filters: []Filter{isFilter(DimensionSubscriber, "sub1")},
			Threshold: 100, CurrentValue: 200,
			Action: action, Breached: true,
		})
	}
	caller := EvalContext{SubscriberID: "sub1"}

	for _, action := range []Action{"block", " BLOCK ", "Block"} {
		err := evaluate(ruleWith(action), caller)
		var ece *ErrCostLimitExceeded
		require.True(t, errors.As(err, &ece), "action %q should block, got %v", action, err)
	}
	for _, action := range []Action{"warn_only", " Throttle "} {
		require.NoError(t, evaluate(ruleWith(action), caller), "action %q is observation only", action)
	}
}

func TestEvaluate_UnrecognizedActionDoesNotBlock(t *testing.T) {
	rules := []Rule{
		{
			RuleID: 1, Filters: []Filter{isFilter(DimensionSubscriber, "sub1")},
			Threshold: 100, CurrentValue: 200,
			Action: "PAUSE", Breached: true,
		},
	}
	require.NoError(t, evaluate(rulesOf(rules...), EvalContext{SubscriberID: "sub1"}))
}

func TestEvaluate_NonMatchingSubscriberIsSkipped(t *testing.T) {
	rules := []Rule{
		{
			RuleID: 1, Filters: []Filter{isFilter(DimensionSubscriber, "other")},
			Threshold: 100, CurrentValue: 200,
			Action: ActionBlock, Breached: true,
		},
	}
	require.NoError(t, evaluate(rulesOf(rules...), EvalContext{SubscriberID: "sub1"}))
}

func TestEvaluate_UnfilteredTeamWideRuleBlocksEveryone(t *testing.T) {
	rules := []Rule{
		{RuleID: 1, Threshold: 100, CurrentValue: 200, Action: ActionBlock, Breached: true},
	}
	for _, ec := range []EvalContext{
		{SubscriberEmail: "alice@example.com", Model: "gpt-4.1-nano", Provider: "OPENAI"},
		{SubscriberID: "sub_bob"},
		{},
	} {
		err := evaluate(rulesOf(rules...), ec)
		var ece *ErrCostLimitExceeded
		require.True(t, errors.As(err, &ece), "caller %+v", ec)
		assert.Equal(t, "1", ece.RuleID)
	}
}

func TestEvaluate_PartialScopeMatchesAnyWithinDimension(t *testing.T) {
	rules := []Rule{
		{
			RuleID: 1, Filters: []Filter{isFilter(DimensionModel, "gpt-4o-mini")},
			Threshold: 100, CurrentValue: 200,
			Action: ActionBlock, Breached: true,
		},
	}
	err := evaluate(rulesOf(rules...), EvalContext{SubscriberID: "any-sub", Model: "gpt-4o-mini", Provider: "OPENAI"})
	require.Error(t, err)
	var ece *ErrCostLimitExceeded
	require.True(t, errors.As(err, &ece))
}

func TestEvaluate_FirstMatchingBlockWins(t *testing.T) {
	rules := []Rule{
		{RuleID: 1, Filters: []Filter{isFilter(DimensionSubscriber, "sub1")}, Threshold: 100, CurrentValue: 200,
			Action: ActionBlock, Breached: true, Name: "first"},
		{RuleID: 2, Filters: []Filter{isFilter(DimensionSubscriber, "sub1")}, Threshold: 50, CurrentValue: 150,
			Action: ActionBlock, Breached: true, Name: "second"},
	}
	err := evaluate(rulesOf(rules...), EvalContext{SubscriberID: "sub1"})
	var ece *ErrCostLimitExceeded
	require.True(t, errors.As(err, &ece))
	assert.Equal(t, "1", ece.RuleID, "first matching rule should win")
}

func rulesOf(rules ...Rule) ruleSet {
	return ruleSet{rules: rules}
}
