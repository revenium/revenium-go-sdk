package enforcement

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// departmentCallScopeFixture is the platform's shared department call-scope
// table; docs/conventions/department-call-scope-fixture.md says where it comes
// from and how to refresh it.
type departmentCallScopeFixture struct {
	Cases []struct {
		Name    string          `json:"name"`
		Payload json.RawMessage `json:"payload"`
		Call    struct {
			SubscriberEmail  string `json:"subscriberEmail"`
			Provider         string `json:"provider"`
			Model            string `json:"model"`
			Agent            string `json:"agent"`
			TaskType         string `json:"taskType"`
			ProductName      string `json:"productName"`
			OrganizationName string `json:"organizationName"`
			CredentialName   string `json:"credentialName"`
			CredentialValue  string `json:"credentialValue"`
		} `json:"call"`
		Expect struct {
			Blocked bool   `json:"blocked"`
			RuleID  *int64 `json:"ruleId"`
		} `json:"expect"`
	} `json:"cases"`
}

// fixtureCallMetadata is the usage metadata a call carrying the fixture's
// fields would have, so the case runs through the SDK's own reading of it.
func fixtureCallMetadata(email string, credential, fields map[string]string) map[string]interface{} {
	subscriber := map[string]interface{}{"email": email}
	given := map[string]interface{}{}
	for key, value := range credential {
		if value != "" {
			given[key] = value
		}
	}
	if len(given) > 0 {
		subscriber["credential"] = given
	}
	metadata := map[string]interface{}{"subscriber": subscriber}
	for key, value := range fields {
		if value != "" {
			metadata[key] = value
		}
	}
	return metadata
}

func TestEvaluate_DepartmentCandidatesFollowThePlatformCallScopeCases(t *testing.T) {
	body, err := os.ReadFile("testdata/department-call-scope-cases.json")
	require.NoError(t, err)
	var fixture departmentCallScopeFixture
	require.NoError(t, json.Unmarshal(body, &fixture))
	require.NotEmpty(t, fixture.Cases)

	for _, tc := range fixture.Cases {
		t.Run(tc.Name, func(t *testing.T) {
			set, err := decodeRuleSet(tc.Payload)
			require.NoError(t, err)
			call := NewEvalContextFromMetadata(context.Background(), fixtureCallMetadata(tc.Call.SubscriberEmail,
				map[string]string{"name": tc.Call.CredentialName, "value": tc.Call.CredentialValue},
				map[string]string{
					"agent": tc.Call.Agent, "taskType": tc.Call.TaskType,
					"productName": tc.Call.ProductName, "organizationName": tc.Call.OrganizationName,
				}), tc.Call.Model, tc.Call.Provider)

			err = evaluate(set, call)

			if !tc.Expect.Blocked {
				assert.NoError(t, err)
				return
			}
			require.NotNil(t, tc.Expect.RuleID, "a blocked case must name its rule")
			var ece *ErrCostLimitExceeded
			require.True(t, errors.As(err, &ece), "expected a block, got %v", err)
			assert.Equal(t, strconv.FormatInt(*tc.Expect.RuleID, 10), ece.RuleID)
		})
	}
}

const (
	openAIDepartmentCap = `{"ruleId": 1, "name": "openai cap", "threshold": 10, "currentValue": 20, "breached": true,
		"shadowMode": false, "action": "BLOCK", "groupBy": "SUBSCRIBER", "departmentId": 7,
		"filters": [{"dimension": "DEPARTMENT", "operator": "IS", "value": "7"},
		            {"dimension": "PROVIDER", "operator": "IS", "value": "openai"}]}`
	wholeDepartmentCap = `{"ruleId": 2, "name": "whole cap", "threshold": 10, "currentValue": 30, "breached": true,
		"shadowMode": false, "action": "BLOCK", "groupBy": "SUBSCRIBER", "departmentId": 7,
		"filters": [{"dimension": "DEPARTMENT", "operator": "IS", "value": "7"}]}`
)

func departmentPayload(rules string, candidates string) string {
	return `{"rules": [` + rules + `],
		"departmentBudgetBlocks": {"alice@example.com": 1},
		"departmentBudgetBlockBalances": {"alice@example.com": 12},
		"departmentBudgetCandidates": ` + candidates + `}`
}

func TestEvaluate_DepartmentCandidateBlocksOnItsOwnBalance(t *testing.T) {
	cases := []struct {
		name       string
		candidates string
		want       expectation
	}{
		{
			name:       "the candidate's balance, not the single slot's or the rule's",
			candidates: `{"alice@example.com": [{"ruleId": 2, "tier": "BLOCK", "departmentId": 7, "balance": "15.5"}]}`,
			want:       blocked(aliceOnAnthropic, "2", 15.5),
		},
		{
			name:       "a null balance falls back to the rule's current value, not the single slot's balance",
			candidates: `{"alice@example.com": [{"ruleId": 2, "tier": "BLOCK", "departmentId": 7, "balance": null}]}`,
			want:       blocked(aliceOnAnthropic, "2", 30),
		},
		{
			name:       "a zero balance is the candidate's balance, not a missing one",
			candidates: `{"alice@example.com": [{"ruleId": 2, "tier": "BLOCK", "departmentId": 7, "balance": 0}]}`,
			want:       blocked(aliceOnAnthropic, "2", 0),
		},
		{
			name:       "a candidate whose rule is missing blocks on the candidate's balance",
			candidates: `{"alice@example.com": [{"ruleId": 99, "tier": "BLOCK", "departmentId": 7, "balance": 4.25}]}`,
			want:       blocked(aliceOnAnthropic, "99", 4.25),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			set, err := decodeRuleSet([]byte(departmentPayload(openAIDepartmentCap+","+wholeDepartmentCap, tc.candidates)))
			require.NoError(t, err)
			assertVerdict(t, set, tc.want)
		})
	}
}

func TestEvaluate_DepartmentCandidatesStopAtTheFirstOneCoveringTheCall(t *testing.T) {
	candidates := `{"alice@example.com": [
		{"ruleId": 1, "tier": "WARN", "departmentId": 7, "balance": 9},
		{"ruleId": 2, "tier": "BLOCK", "departmentId": 7, "balance": 15}]}`
	set, err := decodeRuleSet([]byte(departmentPayload(openAIDepartmentCap+","+wholeDepartmentCap, candidates)))
	require.NoError(t, err)

	t.Run("a warn-tier candidate covering the call ends the scan without blocking", func(t *testing.T) {
		logger := &recordingLogger{}
		previous := core.GetLogger()
		core.SetLogger(logger)
		t.Cleanup(func() { core.SetLogger(previous) })

		assertVerdict(t, set, passes(alice))
		assert.Regexp(t, `rule=1(\D|$)`, logger.warnings())
	})
	t.Run("a warn-tier candidate that does not cover the call passes the call to the next one", func(t *testing.T) {
		assertVerdict(t, set, blocked(aliceOnAnthropic, "2", 15))
	})
	t.Run("a warn-tier candidate whose rule is missing ends the scan without blocking", func(t *testing.T) {
		missing := `{"alice@example.com": [
			{"ruleId": 99, "tier": "WARN", "departmentId": 7, "balance": 9},
			{"ruleId": 2, "tier": "BLOCK", "departmentId": 7, "balance": 15}]}`
		set, err := decodeRuleSet([]byte(departmentPayload(wholeDepartmentCap, missing)))
		require.NoError(t, err)
		assertVerdict(t, set, passes(aliceOnAnthropic))
	})
}

func TestEvaluate_NearLimitWarningIsLoggedOncePerPersonAndRulePerPayload(t *testing.T) {
	candidates := `{"alice@example.com": [{"ruleId": 1, "tier": "WARN", "departmentId": 7, "balance": 9}],
		"bob@example.com": [{"ruleId": 1, "tier": "WARN", "departmentId": 7, "balance": 9}]}`
	payload := departmentPayload(openAIDepartmentCap, candidates)
	set, err := decodeRuleSet([]byte(payload))
	require.NoError(t, err)
	logger := &recordingLogger{}
	previous := core.GetLogger()
	core.SetLogger(logger)
	t.Cleanup(func() { core.SetLogger(previous) })
	bob := EvalContext{SubscriberEmail: "bob@example.com", Provider: "openai"}

	require.NoError(t, evaluate(set, alice))
	require.NoError(t, evaluate(set, alice))
	assert.Equal(t, 1, strings.Count(logger.warnings(), "near its limit"), "a second call by the same person adds no line")

	require.NoError(t, evaluate(set, bob))
	assert.Equal(t, 2, strings.Count(logger.warnings(), "near its limit"), "another person is logged once")

	refreshed, err := decodeRuleSet([]byte(payload))
	require.NoError(t, err)
	require.NoError(t, evaluate(refreshed, alice))
	assert.Equal(t, 3, strings.Count(logger.warnings(), "near its limit"), "a re-decoded payload logs again")
}

func TestEvaluate_ObservationOnlyDepartmentCandidateCoveringTheCallEndsTheScan(t *testing.T) {
	shadowCap := `{"ruleId": 4, "name": "shadow cap", "threshold": 10, "currentValue": 30, "breached": true,
		"shadowMode": true, "action": "BLOCK", "departmentId": 7, "filters": []}`
	candidates := `{"alice@example.com": [
		{"ruleId": 4, "tier": "BLOCK", "departmentId": 7, "balance": 30},
		{"ruleId": 2, "tier": "BLOCK", "departmentId": 7, "balance": 15}]}`
	set, err := decodeRuleSet([]byte(departmentPayload(shadowCap+","+wholeDepartmentCap, candidates)))
	require.NoError(t, err)
	assertVerdict(t, set, passes(aliceOnAnthropic))
}

func TestEvaluate_DepartmentCandidateFiltersIgnoreLetterCase(t *testing.T) {
	scopedCap := func(dimension, operator, value string) string {
		return `{"ruleId": 3, "threshold": 10, "currentValue": 20, "breached": true, "shadowMode": false, "action": "BLOCK",
			"departmentId": 7, "filters": [{"dimension": "` + dimension + `", "operator": "` + operator + `",
			"value": "` + value + `", "values": ["` + value + `"]}]}`
	}
	candidates := `{"alice@example.com": [{"ruleId": 3, "tier": "BLOCK", "departmentId": 7, "balance": 12}]}`
	call := func(set func(*EvalContext)) EvalContext {
		ec := aliceOnAnthropic
		set(&ec)
		return ec
	}
	cases := []struct {
		name      string
		dimension string
		operator  string
		value     string
		want      expectation
	}{
		{"IN, upper-case call", "AGENT", "IN", "claudecode", blocked(call(func(ec *EvalContext) { ec.Agent = "CLAUDECODE" }), "3", 12)},
		{"IN, upper-case filter", "AGENT", "IN", "CLAUDECODE", blocked(call(func(ec *EvalContext) { ec.Agent = "claudecode" }), "3", 12)},
		{"IS_NOT exempts the named agent in another case", "AGENT", "IS_NOT", "ClaudeCode", passes(call(func(ec *EvalContext) { ec.Agent = "CLAUDECODE" }))},
		{"STARTS_WITH, upper-case call", "AGENT", "STARTS_WITH", "claude", blocked(call(func(ec *EvalContext) { ec.Agent = "CLAUDECODE" }), "3", 12)},
		{"ENDS_WITH, upper-case filter", "AGENT", "ENDS_WITH", "CODE", blocked(call(func(ec *EvalContext) { ec.Agent = "claudecode" }), "3", 12)},
		{"PRODUCT", "PRODUCT", "IS", "support-bot", blocked(call(func(ec *EvalContext) { ec.ProductName = "Support-Bot" }), "3", 12)},
		{"ORGANIZATION", "ORGANIZATION", "IS", "ACME", blocked(call(func(ec *EvalContext) { ec.Organization = "acme" }), "3", 12)},
		{"CREDENTIAL", "CREDENTIAL", "IS", "prod-key", blocked(call(func(ec *EvalContext) { ec.Credential = "PROD-KEY" }), "3", 12)},
		{"MODEL", "MODEL", "IS", "claude-haiku-4-5", blocked(call(func(ec *EvalContext) { ec.Model = "Claude-Haiku-4-5" }), "3", 12)},
		{"PROVIDER", "PROVIDER", "IS", "anthropic", blocked(call(func(ec *EvalContext) { ec.Provider = "Anthropic" }), "3", 12)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			set, err := decodeRuleSet([]byte(departmentPayload(scopedCap(tc.dimension, tc.operator, tc.value), candidates)))
			require.NoError(t, err)
			assertVerdict(t, set, tc.want)
		})
	}
}

func TestDecodeRuleSet_DepartmentCandidates(t *testing.T) {
	t.Run("entries under any spelling of the email apply to the caller", func(t *testing.T) {
		candidates := `{" Alice@Example.COM ": [{"ruleId": 1, "tier": "block", "departmentId": 7, "balance": 12}]}`
		set, err := decodeRuleSet([]byte(departmentPayload(openAIDepartmentCap, candidates)))
		require.NoError(t, err)
		assertVerdict(t, set, blocked(alice, "1", 12))
		assertVerdict(t, set, passes(aliceOnAnthropic))
	})
	t.Run("malformed candidates are dropped and the rest still apply", func(t *testing.T) {
		candidates := `{"alice@example.com": [
			{"ruleId": "one", "tier": "BLOCK"}, {"ruleId": 2, "tier": "MAYBE"}, {"tier": "BLOCK"}, 7,
			{"ruleId": 1, "tier": "BLOCK", "departmentId": 7, "balance": 12}]}`
		set, err := decodeRuleSet([]byte(departmentPayload(openAIDepartmentCap+","+wholeDepartmentCap, candidates)))
		require.NoError(t, err)
		assertVerdict(t, set, passes(aliceOnAnthropic))
		assertVerdict(t, set, blocked(alice, "1", 12))
	})
	t.Run("a list with nothing usable falls back to the single slot", func(t *testing.T) {
		candidates := `{"alice@example.com": [{"ruleId": "one", "tier": "BLOCK"}, null]}`
		set, err := decodeRuleSet([]byte(departmentPayload(openAIDepartmentCap, candidates)))
		require.NoError(t, err)
		assertVerdict(t, set, blocked(aliceOnAnthropic, "1", 12))
	})
	t.Run("an entry that is not a list falls back to the single slot", func(t *testing.T) {
		candidates := `{"alice@example.com": {"ruleId": 2, "tier": "BLOCK"}}`
		set, err := decodeRuleSet([]byte(departmentPayload(openAIDepartmentCap, candidates)))
		require.NoError(t, err)
		assertVerdict(t, set, blocked(aliceOnAnthropic, "1", 12))
	})
	t.Run("a candidate list for someone else leaves the caller on the single slot", func(t *testing.T) {
		candidates := `{"bob@example.com": [{"ruleId": 2, "tier": "BLOCK", "balance": 1}]}`
		set, err := decodeRuleSet([]byte(departmentPayload(openAIDepartmentCap, candidates)))
		require.NoError(t, err)
		assertVerdict(t, set, blocked(aliceOnAnthropic, "1", 12))
	})
}

func TestParseCandidate_RuleIDFormsThePlatformCanSerialize(t *testing.T) {
	cases := []struct {
		name       string
		candidates string
		want       expectation
	}{
		{
			name:       "a rule id written as a string is read, not dropped",
			candidates: `{"alice@example.com": [{"ruleId": "1", "tier": "BLOCK", "departmentId": 7, "balance": 12}]}`,
			want:       passes(aliceOnAnthropic),
		},
		{
			name:       "a rule id of zero is dropped, as the platform's own usability rule drops it",
			candidates: `{"alice@example.com": [{"ruleId": 0, "tier": "BLOCK", "departmentId": 7, "balance": 5}]}`,
			want:       blocked(aliceOnAnthropic, "1", 12),
		},
		{
			name:       "a negative rule id is dropped",
			candidates: `{"alice@example.com": [{"ruleId": -3, "tier": "BLOCK", "departmentId": 7, "balance": 5}]}`,
			want:       blocked(aliceOnAnthropic, "1", 12),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			set, err := decodeRuleSet([]byte(departmentPayload(openAIDepartmentCap, tc.candidates)))
			require.NoError(t, err)
			assertVerdict(t, set, tc.want)
		})
	}
}

func TestEvaluate_DepartmentCandidateOnADimensionTheCallOmits(t *testing.T) {
	scopedCap := func(dimension string) string {
		return `{"ruleId": 5, "threshold": 10, "currentValue": 20, "breached": true, "shadowMode": false,
			"action": "BLOCK", "departmentId": 7,
			"filters": [{"dimension": "` + dimension + `", "operator": "IS", "value": "anything"}]}`
	}
	candidates := `{"alice@example.com": [{"ruleId": 5, "tier": "BLOCK", "departmentId": 7, "balance": 12}]}`

	for _, dimension := range []string{"PRODUCT", "ORGANIZATION", "TASK_TYPE", "AGENT", "CREDENTIAL"} {
		t.Run(dimension+" cap lets a call that carries no "+dimension+" through", func(t *testing.T) {
			set, err := decodeRuleSet([]byte(departmentPayload(scopedCap(dimension), candidates)))
			require.NoError(t, err)
			assertVerdict(t, set, passes(aliceOnAnthropic))
		})
	}
}

func TestEvaluate_DepartmentCandidatesAreSafeUnderConcurrentCalls(t *testing.T) {
	candidates := `{"alice@example.com": [{"ruleId": 1, "tier": "WARN", "departmentId": 7, "balance": 9},
		{"ruleId": 2, "tier": "BLOCK", "departmentId": 7, "balance": 15}]}`
	set, err := decodeRuleSet([]byte(departmentPayload(openAIDepartmentCap+","+wholeDepartmentCap, candidates)))
	require.NoError(t, err)

	callers := []EvalContext{alice, aliceOnAnthropic}
	var group sync.WaitGroup
	for worker := 0; worker < 32; worker++ {
		group.Add(1)
		go func(caller EvalContext) {
			defer group.Done()
			for call := 0; call < 200; call++ {
				_ = evaluate(set, caller)
			}
		}(callers[worker%len(callers)])
	}
	group.Wait()
}
