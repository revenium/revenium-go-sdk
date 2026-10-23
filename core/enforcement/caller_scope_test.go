package enforcement

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	alice            = EvalContext{SubscriberEmail: "alice@example.com", Model: "gpt-4.1-nano", Provider: "OPENAI"}
	aliceUpper       = EvalContext{SubscriberEmail: "Alice@Example.com", Model: "gpt-4.1-nano", Provider: "OPENAI"}
	aliceWithID      = EvalContext{SubscriberID: "sub_123", SubscriberEmail: "alice@example.com", Model: "gpt-4.1-nano", Provider: "OPENAI"}
	aliceOnAnthropic = EvalContext{SubscriberEmail: "alice@example.com", Model: "claude-haiku-4-5", Provider: "ANTHROPIC"}
	bob              = EvalContext{SubscriberEmail: "bob@example.com", Model: "gpt-4.1-nano", Provider: "OPENAI"}
	carol            = EvalContext{SubscriberEmail: "carol@example.com", Model: "gpt-4.1-nano", Provider: "OPENAI"}
	anonymous        = EvalContext{Model: "gpt-4.1-nano", Provider: "OPENAI"}
)

const (
	perPersonOpenAI = `{"rules": [{
		"ruleId": 7, "name": "per-dev", "metricType": "TOTAL_COST", "threshold": 50, "currentValue": 61,
		"periodType": "MONTHLY", "breached": true, "shadowMode": false, "action": "BLOCK", "groupBy": "SUBSCRIBER",
		"filters": [{"dimension": "PROVIDER", "operator": "IS", "value": "OPENAI", "values": []}],
		"groupBreakdown": [
			{"groupValue": "alice@example.com", "displayName": "alice@example.com", "currentValue": 61, "breached": true},
			{"groupValue": "bob@example.com", "displayName": "bob@example.com", "currentValue": 3, "breached": false}
		]}]}`

	departmentBudget = `{"rules": [
		{"ruleId": 12, "name": "dept-173", "threshold": 500, "currentValue": 512, "breached": true,
		 "shadowMode": false, "action": "BLOCK", "groupBy": "DEPARTMENT", "departmentId": 173,
		 "filters": [{"dimension": "PROVIDER", "operator": "IS", "value": "OPENAI"}]}],
		"departmentBudgetBlocks": {"alice@example.com": 12},
		"departmentBudgetBlockBalances": {"alice@example.com": "41.50"}}`
)

type expectation struct {
	caller       EvalContext
	blockedBy    string
	currentValue float64
}

func blocked(caller EvalContext, ruleID string, currentValue float64) expectation {
	return expectation{caller: caller, blockedBy: ruleID, currentValue: currentValue}
}

func passes(caller EvalContext) expectation {
	return expectation{caller: caller}
}

// TestEvaluate_DecidesEachBreachedRuleForTheCallingPerson pins, per rule
// shape, who a breached rule blocks. The verdicts follow the platform's
// EnforcementEvaluationService.
func TestEvaluate_DecidesEachBreachedRuleForTheCallingPerson(t *testing.T) {
	cases := []struct {
		name    string
		payload string
		expect  []expectation
	}{
		{
			name:    "team-wide cap with no filters blocks every caller",
			payload: `{"rules": [{"ruleId": 1, "threshold": 100, "currentValue": 120, "breached": true, "shadowMode": false, "action": "BLOCK", "filters": []}]}`,
			expect:  []expectation{blocked(alice, "1", 120), blocked(bob, "1", 120), blocked(anonymous, "1", 120)},
		},
		{
			name: "per-person cap with no filters blocks only the person over it",
			payload: `{"rules": [{"ruleId": 2, "metricType": "TOTAL_COST", "threshold": 50, "currentValue": 61, "breached": true,
				"shadowMode": false, "action": "BLOCK", "groupBy": "SUBSCRIBER", "filters": [],
				"groupBreakdown": [
					{"groupValue": "alice@example.com", "currentValue": 61, "breached": true},
					{"groupValue": "bob@example.com", "currentValue": 3, "breached": false}]}]}`,
			expect: []expectation{blocked(alice, "2", 61), passes(bob), passes(carol), passes(anonymous)},
		},
		{
			name:    "per-person cap with a provider filter blocks only the person over it, on that provider",
			payload: perPersonOpenAI,
			expect:  []expectation{blocked(alice, "7", 61), blocked(aliceUpper, "7", 61), passes(bob), passes(carol), passes(aliceOnAnthropic)},
		},
		{
			name: "per-person entry filed under the subscriber id matches a caller sending id and email",
			payload: `{"rules": [{"ruleId": 3, "metricType": "TOTAL_COST", "threshold": 50, "currentValue": 61, "breached": true,
				"shadowMode": false, "action": "BLOCK", "groupBy": "SUBSCRIBER",
				"groupBreakdown": [{"groupValue": "sub_123", "currentValue": 61, "breached": true}]}]}`,
			expect: []expectation{blocked(aliceWithID, "3", 61), passes(alice)},
		},
		{
			name: "per-person entry breached blocks even when the rule-level flag is not set",
			payload: `{"rules": [{"ruleId": 4, "metricType": "TOTAL_COST", "threshold": 50, "currentValue": 49, "breached": false,
				"shadowMode": false, "action": "BLOCK", "groupBy": "SUBSCRIBER",
				"groupBreakdown": [{"groupValue": "alice@example.com", "currentValue": 52, "breached": true}]}]}`,
			expect: []expectation{blocked(aliceUpper, "4", 52), passes(bob)},
		},
		{
			name: "per-person cost cap without a breakdown is left to the platform",
			payload: `{"rules": [{"ruleId": 5, "metricType": "TOTAL_COST", "threshold": 50, "currentValue": 61, "breached": true,
				"shadowMode": false, "action": "BLOCK", "groupBy": "SUBSCRIBER"}]}`,
			expect: []expectation{passes(alice), passes(bob)},
		},
		{
			name: "per-person rule on a non-cost metric is pooled, as on the platform",
			payload: `{"rules": [{"ruleId": 6, "metricType": "TOKEN_COUNT", "threshold": 1000, "currentValue": 1200, "breached": true,
				"shadowMode": false, "action": "BLOCK", "groupBy": "SUBSCRIBER"}]}`,
			expect: []expectation{blocked(alice, "6", 1200), blocked(bob, "6", 1200)},
		},
		{
			name:    "department budget blocks only the people the platform lists, on their own balance",
			payload: departmentBudget,
			expect:  []expectation{blocked(alice, "12", 41.5), blocked(aliceUpper, "12", 41.5), blocked(aliceOnAnthropic, "12", 41.5), passes(bob), passes(carol), passes(anonymous)},
		},
		{
			name: "department ancestor cap with a DEPARTMENT filter blocks only the people the platform lists",
			payload: `{"rules": [{"ruleId": 13, "threshold": 500, "currentValue": 512, "breached": true, "shadowMode": false,
				"action": "BLOCK", "departmentId": 173,
				"filters": [{"dimension": "DEPARTMENT", "operator": "IS", "value": "173"}]}],
				"departmentBudgetBlocks": {"alice@example.com": 13}}`,
			expect: []expectation{blocked(alice, "13", 512), passes(carol)},
		},
		{
			name: "department block map under the pre-rename spelling",
			payload: `{"rules": [{"ruleId": 14, "threshold": 500, "currentValue": 512, "breached": true, "shadowMode": false,
				"action": "BLOCK", "groupBy": "ORG_UNIT"}], "orgUnitBudgetBlocks": {"ALICE@example.com": 14}}`,
			expect: []expectation{blocked(alice, "14", 512), passes(bob)},
		},
		{
			name:    "department block naming a rule missing from the payload still blocks",
			payload: `{"rules": [], "departmentBudgetBlocks": {"alice@example.com": 99}, "departmentBudgetBlockBalances": {"alice@example.com": 7.25}}`,
			expect:  []expectation{blocked(alice, "99", 7.25), passes(bob)},
		},
		{
			name: "department block on a shadow rule does not block",
			payload: `{"rules": [{"ruleId": 15, "threshold": 500, "currentValue": 512, "breached": true, "shadowMode": true,
				"action": "BLOCK", "groupBy": "DEPARTMENT"}], "departmentBudgetBlocks": {"alice@example.com": 15}}`,
			expect: []expectation{passes(alice)},
		},
		{
			name: "department block on a warn-only rule does not block",
			payload: `{"rules": [{"ruleId": 16, "threshold": 500, "currentValue": 512, "breached": true, "shadowMode": false,
				"action": "WARN_ONLY", "groupBy": "DEPARTMENT"}], "departmentBudgetBlocks": {"alice@example.com": 16}}`,
			expect: []expectation{passes(alice)},
		},
		{
			name: "rule on a person's email blocks that person whatever else the call carries",
			payload: `{"rules": [{"ruleId": 101, "threshold": 10, "currentValue": 25, "breached": true, "shadowMode": false, "action": "BLOCK",
				"filters": [{"dimension": "SUBSCRIBER", "operator": "IS", "value": "alice@example.com"}]}]}`,
			expect: []expectation{blocked(alice, "101", 25), blocked(aliceUpper, "101", 25), blocked(aliceWithID, "101", 25), passes(bob)},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			set, err := decodeRuleSet([]byte(tc.payload))
			require.NoError(t, err)
			for _, want := range tc.expect {
				assertVerdict(t, set, want)
			}
		})
	}
}

// TestEvaluate_HonoursEveryFilterWithoutWideningTheRule covers rules whose
// scope goes beyond a single IS filter.
func TestEvaluate_HonoursEveryFilterWithoutWideningTheRule(t *testing.T) {
	agent := func(name string) EvalContext {
		ec := bob
		ec.Agent = name
		return ec
	}
	model := func(name string) EvalContext {
		ec := bob
		ec.Model = name
		return ec
	}

	cases := []struct {
		name    string
		filters string
		expect  []expectation
	}{
		{
			name:    "AGENT IS batch-bot blocks only that agent",
			filters: `{"dimension": "PROVIDER", "operator": "IS", "value": "OPENAI"}, {"dimension": "AGENT", "operator": "IS", "value": "batch-bot"}`,
			expect:  []expectation{blocked(agent("batch-bot"), "7", 61), passes(agent("other-bot")), passes(bob)},
		},
		{
			name:    "SUBSCRIBER IN [alice, carol] blocks only those people",
			filters: `{"dimension": "PROVIDER", "operator": "IS", "value": "OPENAI"}, {"dimension": "SUBSCRIBER", "operator": "IN", "value": "alice@example.com,carol@example.com", "values": ["alice@example.com", "carol@example.com"]}`,
			expect:  []expectation{blocked(alice, "7", 61), blocked(carol, "7", 61), passes(bob)},
		},
		{
			name:    "SUBSCRIBER IN [alice] alone blocks alice",
			filters: `{"dimension": "SUBSCRIBER", "operator": "IN", "value": "alice@example.com", "values": ["alice@example.com"]}`,
			expect:  []expectation{blocked(aliceUpper, "7", 61), passes(bob)},
		},
		{
			name:    "MODEL IS_NOT a dated model leaves calls naming that snapshot alone",
			filters: `{"dimension": "PROVIDER", "operator": "IS", "value": "OPENAI"}, {"dimension": "MODEL", "operator": "IS_NOT", "value": "gpt-4.1-nano-2025-04-14"}`,
			expect: []expectation{passes(model("gpt-4.1-nano-2025-04-14")), blocked(model("gpt-4.1-nano"), "7", 61),
				blocked(model("gpt-4o"), "7", 61)},
		},
		{
			name:    "MODEL STARTS_WITH gpt-4o blocks only that family",
			filters: `{"dimension": "MODEL", "operator": "STARTS_WITH", "value": "gpt-4o"}`,
			expect:  []expectation{blocked(model("gpt-4o-mini"), "7", 61), passes(model("gpt-4.1-nano"))},
		},
		{
			name:    "a null-valued filter on a dimension the SDK cannot see never blocks",
			filters: `{"dimension": "WORKSPACE", "operator": "IS", "value": null}`,
			expect:  []expectation{passes(alice), passes(bob)},
		},
		{
			name:    "an empty IN on the department dimension never blocks",
			filters: `{"dimension": "DEPARTMENT", "operator": "IN", "values": []}`,
			expect:  []expectation{passes(alice), passes(bob)},
		},
		{
			name:    "an unknown operator without a value never blocks",
			filters: `{"dimension": "MODEL", "operator": "MATCHES"}`,
			expect:  []expectation{passes(alice), passes(bob)},
		},
		{
			name:    "a filter on a dimension the SDK cannot see never blocks",
			filters: `{"dimension": "WORKSPACE", "operator": "IS", "value": "w1"}`,
			expect:  []expectation{passes(alice), passes(bob)},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			payload := fmt.Sprintf(`{"rules": [{"ruleId": 7, "threshold": 50, "currentValue": 61, "breached": true,
				"shadowMode": false, "action": "BLOCK", "filters": [%s]}]}`, tc.filters)
			set, err := decodeRuleSet([]byte(payload))
			require.NoError(t, err)
			for _, want := range tc.expect {
				assertVerdict(t, set, want)
			}
		})
	}
}

func TestEvaluate_ValidRulesDoNotWarnAboutGlobalBlocks(t *testing.T) {
	logger := &recordingLogger{}
	previous := core.GetLogger()
	core.SetLogger(logger)
	t.Cleanup(func() { core.SetLogger(previous) })

	for _, payload := range []string{
		`{"rules": [{"ruleId": 1, "threshold": 100, "currentValue": 120, "breached": true, "shadowMode": false, "action": "BLOCK", "filters": []}]}`,
		`{"rules": [{"ruleId": 2, "metricType": "TOTAL_COST", "threshold": 50, "currentValue": 61, "breached": true,
			"shadowMode": false, "action": "BLOCK", "groupBy": "SUBSCRIBER", "filters": [],
			"groupBreakdown": [{"groupValue": "alice@example.com", "currentValue": 61, "breached": true}]}]}`,
		`{"rules": [{"ruleId": 12, "threshold": 500, "currentValue": 512, "breached": true, "shadowMode": false,
			"action": "BLOCK", "groupBy": "DEPARTMENT", "filters": []}], "departmentBudgetBlocks": {"alice@example.com": 12}}`,
	} {
		set, err := decodeRuleSet([]byte(payload))
		require.NoError(t, err)
		_ = evaluate(set, bob)
	}
	assert.Empty(t, logger.warnings())
}

func assertVerdict(t *testing.T, set ruleSet, want expectation) {
	t.Helper()
	err := evaluate(set, want.caller)
	if want.blockedBy == "" {
		assert.NoError(t, err, "caller %+v should pass", want.caller)
		return
	}
	var ece *ErrCostLimitExceeded
	if !assert.True(t, errors.As(err, &ece), "caller %+v should be blocked, got %v", want.caller, err) {
		return
	}
	assert.Equal(t, want.blockedBy, ece.RuleID, "caller %+v", want.caller)
	assert.Equal(t, want.currentValue, ece.CurrentValue, "caller %+v", want.caller)
}

type recordingLogger struct {
	mu   sync.Mutex
	warn []string
	errs []string
}

func (l *recordingLogger) Debug(string, ...interface{}) {}
func (l *recordingLogger) Info(string, ...interface{})  {}
func (l *recordingLogger) SetLevel(core.LogLevel)       {}
func (l *recordingLogger) GetLevel() core.LogLevel      { return core.LogLevelDebug }

func (l *recordingLogger) Warn(message string, args ...interface{}) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.warn = append(l.warn, fmt.Sprintf(message, args...))
}

func (l *recordingLogger) warnings() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return strings.Join(l.warn, "\n")
}

func (l *recordingLogger) Error(message string, args ...interface{}) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.errs = append(l.errs, fmt.Sprintf(message, args...))
}

func (l *recordingLogger) errors() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return strings.Join(l.errs, "\n")
}
