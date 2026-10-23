package enforcement

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/revenium/revenium-go-sdk/core/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testTeamID = "hashed-team-id-abc123"

func resetEngines() {
	Stop()
	enginesMu.Lock()
	defer enginesMu.Unlock()
	unreleased = map[engineKey]map[*lease]struct{}{}
}

// writeEnvelope writes the `{rules: [...]}` envelope the server emits.
func writeEnvelope(w http.ResponseWriter, rules []Rule) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(struct {
		Rules []Rule `json:"rules"`
	}{Rules: rules})
}

func TestEngine_StartAndCheck(t *testing.T) {
	rules := []Rule{
		{
			RuleID: 1, Name: "monthly", Filters: []Filter{isFilter(DimensionSubscriber, "sub1")}, PeriodType: "MONTHLY",
			Threshold: 100, CurrentValue: 200, Action: ActionBlock, Breached: true,
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, core.EnforcementRulesPathPrefix+testTeamID, r.URL.Path)
		assert.Equal(t, "test-key", r.Header.Get("x-api-key"))
		writeEnvelope(w, rules)
	}))
	defer srv.Close()

	resetEngines()
	e := Start(srv.URL, "test-key", testTeamID)
	require.NotNil(t, e)
	defer Stop()

	err := e.Check(EvalContext{SubscriberID: "sub1"})
	require.Error(t, err)
	var ece *ErrCostLimitExceeded
	require.True(t, errors.As(err, &ece))
	assert.Equal(t, "1", ece.RuleID)

	// Unrelated subscriber should be allowed
	require.NoError(t, e.Check(EvalContext{SubscriberID: "other"}))
}

func TestEngine_StartIdempotent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeEnvelope(w, []Rule{})
	}))
	defer srv.Close()

	resetEngines()
	e1 := Start(srv.URL, "key", testTeamID)
	e2 := Start(srv.URL, "key", testTeamID)
	assert.Equal(t, e1, e2, "Start should return same instance")
	Stop()
}

func TestEngine_StopClearsInstance(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeEnvelope(w, []Rule{})
	}))
	defer srv.Close()

	resetEngines()
	Start(srv.URL, "key", testTeamID)
	Stop()
	assert.Nil(t, Get())
}

func TestEngine_FetchErrorFallsBack(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	resetEngines()
	e := Start(srv.URL, "key", testTeamID)
	defer Stop()

	// Should still allow when fetch fails (no rules loaded — fails open)
	require.NoError(t, e.Check(EvalContext{SubscriberID: "sub1"}))
}

func TestEngine_ManualRefreshUpdatesCache(t *testing.T) {
	var callCount int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&callCount, 1)
		if n == 1 {
			// First call: not breached
			writeEnvelope(w, []Rule{
				{RuleID: 1, Filters: []Filter{isFilter(DimensionSubscriber, "sub1")}, Threshold: 100, CurrentValue: 50,
					Action: ActionBlock, Breached: false},
			})
		} else {
			// Subsequent calls: breached
			writeEnvelope(w, []Rule{
				{RuleID: 1, Filters: []Filter{isFilter(DimensionSubscriber, "sub1")}, Threshold: 100, CurrentValue: 200,
					Action: ActionBlock, Breached: true},
			})
		}
	}))
	defer srv.Close()

	resetEngines()
	e := Start(srv.URL, "key", testTeamID)
	defer Stop()

	require.NoError(t, e.Check(EvalContext{SubscriberID: "sub1"}))

	require.NoError(t, e.poller().fetchRules(context.Background()))

	err := e.Check(EvalContext{SubscriberID: "sub1"})
	require.Error(t, err)
	var ece *ErrCostLimitExceeded
	require.True(t, errors.As(err, &ece))
}

func TestEngine_NoContentCachesEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, core.EnforcementRulesPathPrefix+testTeamID, r.URL.Path)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	resetEngines()
	e := Start(srv.URL, "key", testTeamID)
	defer Stop()

	require.NoError(t, e.Check(EvalContext{SubscriberID: "sub1"}))
}

func TestEngine_RejectsNonHTTPBaseURL(t *testing.T) {
	// Non-http(s) schemes or schemeless strings must not cause the
	// engine to ship the API key anywhere — Start should no-op with a
	// warning, and Check should still be callable (returns nil = allowed).
	for _, url := range []string{"file:///etc/passwd", "ftp://example.com", "not-a-url", ""} {
		resetEngines()
		e := Start(url, "key", testTeamID)
		require.NotNil(t, e)
		require.NoError(t, e.Check(EvalContext{SubscriberID: "sub1"}),
			"Check() should allow when baseURL %q was rejected", url)
		Stop()
	}
}

func TestEngine_MissingTeamIDSkipsFetch(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	resetEngines()
	e := Start(srv.URL, "key", "")
	defer Stop()

	require.NotNil(t, e)
	assert.Equal(t, int32(0), atomic.LoadInt32(&hits), "no HTTP calls should be made without teamID")

	require.NoError(t, e.poller().fetchRules(context.Background()))
	require.NoError(t, e.Check(EvalContext{SubscriberID: "sub1"}))
}

func TestEngine_PackageLevelCheckAllowsWhenDormant(t *testing.T) {
	// Get() returns nil before Start, and the package-level Check convenience
	// must tolerate that (enforcement fails open by design).
	resetEngines()
	require.NoError(t, Check(EvalContext{SubscriberID: "sub1"}))
}

func TestEngine_ShadowRuleDoesNotBlock(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeEnvelope(w, []Rule{
			{
				RuleID: 99, Name: "shadow-test", Filters: []Filter{isFilter(DimensionSubscriber, "sub1")},
				Threshold: 100, CurrentValue: 500, Action: ActionBlock,
				Breached: true, ShadowMode: true,
			},
		})
	}))
	defer srv.Close()

	resetEngines()
	e := Start(srv.URL, "key", testTeamID)
	defer Stop()

	require.NoError(t, e.Check(EvalContext{SubscriberID: "sub1"}),
		"shadowMode=true must never block even when breached and action=BLOCK")
}

func TestEngine_EachTeamGetsItsOwnEngineAndRules(t *testing.T) {
	rules := testutil.NewMockRulesServer()
	defer rules.Close()
	rules.SetPayload("team-a", `{"rules": []}`)
	rules.SetPayload("team-b", `{"rules": [{"ruleId": 4211, "threshold": 10, "currentValue": 12, "breached": true,
		"shadowMode": false, "action": "BLOCK", "filters": [{"dimension": "SUBSCRIBER", "operator": "IS", "value": "alice@example.com"}]}]}`)

	resetEngines()
	defer Stop()
	a := Start(rules.URL(), "key-a", "team-a")
	b := Start(rules.URL(), "key-b", "team-b")
	require.NotSame(t, a, b)

	require.NoError(t, a.Check(alice))
	var ece *ErrCostLimitExceeded
	require.True(t, errors.As(b.Check(alice), &ece))
	assert.Equal(t, "4211", ece.RuleID)
	assert.Equal(t, 1, rules.Fetches("team-a"))
	assert.Equal(t, 1, rules.Fetches("team-b"))
}

func TestEngine_SameCredentialsShareOneEngineAndOneFetch(t *testing.T) {
	rules := testutil.NewMockRulesServer()
	defer rules.Close()
	rules.SetPayload("team-a", `{"rules": []}`)

	resetEngines()
	defer Stop()
	first := Start(rules.URL(), "key-a", "team-a")
	second := Start(rules.URL(), "key-a", "team-a")
	assert.Same(t, first.poller(), second.poller())
	assert.Equal(t, 1, rules.Fetches("team-a"))
}

func TestEngine_DormantFirstEngineDoesNotDisableLaterTeams(t *testing.T) {
	rules := testutil.NewMockRulesServer()
	defer rules.Close()
	rules.SetPayload("team-b", `{"rules": [{"ruleId": 9, "threshold": 1, "currentValue": 2, "breached": true,
		"shadowMode": false, "action": "BLOCK", "filters": []}]}`)

	resetEngines()
	defer Stop()
	dormant := Start(rules.URL(), "key-a", "")
	active := Start(rules.URL(), "key-b", "team-b")

	require.NoError(t, dormant.Check(EvalContext{}))
	require.Error(t, active.Check(EvalContext{}))
	assert.Same(t, dormant.poller(), Get().poller(), "the deprecated Get keeps returning the first engine")
}

func TestStartFor_FallsBackToTheMeteringBaseURL(t *testing.T) {
	rules := testutil.NewMockRulesServer()
	defer rules.Close()
	rules.SetPayload("team-a", `{"rules": []}`)

	resetEngines()
	defer Stop()
	assert.Nil(t, StartFor(nil))
	e := StartFor(&core.ReveniumConfig{APIKey: "key-a", BaseURL: rules.URL(), TeamID: "team-a"})
	require.NotNil(t, e)
	assert.Equal(t, 1, rules.Fetches("team-a"))
	reused := StartFor(&core.ReveniumConfig{APIKey: "key-a", BaseURL: "http://unused.invalid", EnforcementBaseURL: rules.URL(), TeamID: "team-a"})
	assert.Same(t, e.poller(), reused.poller())
}

func TestEngine_StopAllowsHeldEnginesAndRestartLoadsFreshRules(t *testing.T) {
	rules := testutil.NewMockRulesServer()
	defer rules.Close()
	rules.SetPayload("team-a", `{"rules": [{"ruleId": 1, "threshold": 1, "currentValue": 2, "breached": true,
		"shadowMode": false, "action": "BLOCK", "filters": []}]}`)

	resetEngines()
	defer Stop()
	held := Start(rules.URL(), "key-a", "team-a")
	var ece *ErrCostLimitExceeded
	require.True(t, errors.As(held.Check(EvalContext{}), &ece))
	assert.Equal(t, "1", ece.RuleID)

	Stop()
	require.NoError(t, held.Check(EvalContext{}), "a stopped engine must not keep blocking on its old rules")

	rules.SetPayload("team-a", `{"rules": [{"ruleId": 2, "threshold": 1, "currentValue": 3, "breached": true,
		"shadowMode": false, "action": "BLOCK", "filters": []}]}`)
	restarted := Start(rules.URL(), "key-a", "team-a")
	require.True(t, errors.As(held.Check(EvalContext{}), &ece))
	assert.Equal(t, "2", ece.RuleID)
	assert.Equal(t, 2, rules.Fetches("team-a"))

	held.Release()
	require.True(t, errors.As(restarted.Check(EvalContext{}), &ece), "a lease taken before Stop must not end the restarted engine")
}

func TestStart_SlowTeamDoesNotDelayAnotherTeam(t *testing.T) {
	slowFetchStarted := make(chan struct{})
	releaseSlowFetch := make(chan struct{})
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(slowFetchStarted)
		select {
		case <-releaseSlowFetch:
		case <-r.Context().Done():
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer slow.Close()
	fast := testutil.NewMockRulesServer()
	defer fast.Close()

	resetEngines()
	defer Stop()
	defer close(releaseSlowFetch)

	slowStarted := make(chan *Engine, 1)
	go func() { slowStarted <- Start(slow.URL, "key-a", "team-a") }()
	<-slowFetchStarted

	began := time.Now()
	StartFor(&core.ReveniumConfig{APIKey: "key-b", BaseURL: fast.URL(), TeamID: "team-b"})
	assert.Less(t, time.Since(began), 500*time.Millisecond, "team B waited on team A's rule fetch")
	assert.Equal(t, 1, fast.Fetches("team-b"))

	select {
	case <-slowStarted:
		t.Fatal("team A's Start returned before its initial fetch finished")
	default:
	}
	releaseSlowFetch <- struct{}{}
	<-slowStarted
}

func TestEngine_LastReleaseStopsOnlyThatTeamsPoll(t *testing.T) {
	rules := testutil.NewMockRulesServer()
	defer rules.Close()
	blocking := `{"rules": [{"ruleId": 1, "threshold": 1, "currentValue": 2, "breached": true,
		"shadowMode": false, "action": "BLOCK", "filters": []}]}`
	rules.SetPayload("team-a", blocking)
	rules.SetPayload("team-b", blocking)

	resetEngines()
	defer Stop()
	first := Start(rules.URL(), "key-a", "team-a")
	second := Start(rules.URL(), "key-a", "team-a")
	other := Start(rules.URL(), "key-b", "team-b")
	shared := first.poller()

	first.Release()
	first.Release()
	require.Error(t, second.Check(EvalContext{}), "the remaining client keeps its team's rules")
	assert.Same(t, shared, second.poller())
	assert.False(t, shared.isStopped())

	second.Release()
	assert.Nil(t, second.poller(), "the last release drops the team's engine")
	assert.True(t, shared.isStopped(), "the last release stops the team's poll")
	require.NoError(t, second.Check(EvalContext{}))
	require.Error(t, other.Check(EvalContext{}), "another team is untouched")

	Start(rules.URL(), "key-a", "team-a").Release()
	assert.Equal(t, 2, rules.Fetches("team-a"), "a new client after the last release loads the rules again")
}

func TestEngine_HandleHeldAcrossStopKeepsTheRestartedPollAlive(t *testing.T) {
	rules := testutil.NewMockRulesServer()
	defer rules.Close()
	rules.SetPayload("team-a", `{"rules": [{"ruleId": 1, "threshold": 1, "currentValue": 2, "breached": true,
		"shadowMode": false, "action": "BLOCK", "filters": []}]}`)

	resetEngines()
	defer resetEngines()
	clientA := Start(rules.URL(), "key-a", "team-a")
	require.Error(t, clientA.Check(EvalContext{}))

	Stop()
	require.NoError(t, clientA.Check(EvalContext{}))

	clientB := Start(rules.URL(), "key-a", "team-a")
	require.Error(t, clientA.Check(EvalContext{}))

	clientB.Release()
	require.Error(t, clientA.Check(EvalContext{}), "closing B must not end the poll A still relies on")
	restarted := clientA.poller()
	require.NotNil(t, restarted)

	clientA.Release()
	assert.Nil(t, clientA.poller())
	assert.True(t, restarted.isStopped(), "A's close stops the poll")
}

func TestPackageCheck_FollowsTheEarliestTeamStillRunning(t *testing.T) {
	rules := testutil.NewMockRulesServer()
	defer rules.Close()
	rules.SetPayload("team-a", `{"rules": []}`)
	rules.SetPayload("team-b", `{"rules": [{"ruleId": 9, "threshold": 1, "currentValue": 2, "breached": true,
		"shadowMode": false, "action": "BLOCK", "filters": []}]}`)

	resetEngines()
	defer resetEngines()
	first := Start(rules.URL(), "key-a", "team-a")
	second := Start(rules.URL(), "key-b", "team-b")
	require.NoError(t, Check(EvalContext{}), "the earliest team has no rules")

	first.Release()
	Get().Release()
	var ece *ErrCostLimitExceeded
	require.True(t, errors.As(Check(EvalContext{}), &ece), "the package-level Check moves on to the team still running")
	assert.Equal(t, "9", ece.RuleID)

	second.Release()
	assert.Nil(t, Get())
	require.NoError(t, Check(EvalContext{}))
}

func TestStart_TrimsLegacyProfitstreamSuffixFromBaseURL(t *testing.T) {
	rules := testutil.NewMockRulesServer()
	defer rules.Close()
	rules.SetPayload(testTeamID, `{"rules": [{"ruleId": 1, "threshold": 1, "currentValue": 2, "breached": true,
		"shadowMode": false, "action": "BLOCK", "filters": []}]}`)

	resetEngines()
	defer resetEngines()
	e := Start(rules.URL()+"/profitstream", "key", testTeamID)
	require.Error(t, e.Check(EvalContext{}), "a base URL carrying the old /profitstream workaround must still reach the rules endpoint")
	assert.Equal(t, 1, rules.Fetches(testTeamID))
}
