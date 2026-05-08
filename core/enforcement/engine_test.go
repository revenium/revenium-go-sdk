package enforcement

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testTeamID = "hashed-team-id-abc123"

func resetSingleton() {
	instanceMu.Lock()
	prev := instance
	instance = nil
	instanceMu.Unlock()
	// Cancel the previous engine's poll goroutine so tests can't leak
	// HTTP calls into a torn-down httptest server.
	if prev != nil {
		prev.cancel()
		prev.wg.Wait()
	}
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
			RuleID: 1, Name: "monthly", SubscriberID: "sub1", PeriodType: "MONTHLY",
			Threshold: 100, CurrentValue: 200, Action: ActionBlock, Breached: true,
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, enforcementPathPrefix+testTeamID, r.URL.Path)
		assert.Equal(t, "test-key", r.Header.Get("x-api-key"))
		writeEnvelope(w, rules)
	}))
	defer srv.Close()

	resetSingleton()
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

	resetSingleton()
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

	resetSingleton()
	Start(srv.URL, "key", testTeamID)
	Stop()
	assert.Nil(t, Get())
}

func TestEngine_FetchErrorFallsBack(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	resetSingleton()
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
				{RuleID: 1, SubscriberID: "sub1", Threshold: 100, CurrentValue: 50,
					Action: ActionBlock, Breached: false},
			})
		} else {
			// Subsequent calls: breached
			writeEnvelope(w, []Rule{
				{RuleID: 1, SubscriberID: "sub1", Threshold: 100, CurrentValue: 200,
					Action: ActionBlock, Breached: true},
			})
		}
	}))
	defer srv.Close()

	resetSingleton()
	e := Start(srv.URL, "key", testTeamID)
	defer Stop()

	require.NoError(t, e.Check(EvalContext{SubscriberID: "sub1"}))

	require.NoError(t, e.fetchRules(context.Background()))

	err := e.Check(EvalContext{SubscriberID: "sub1"})
	require.Error(t, err)
	var ece *ErrCostLimitExceeded
	require.True(t, errors.As(err, &ece))
}

func TestEngine_NoContentCachesEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, enforcementPathPrefix+testTeamID, r.URL.Path)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	resetSingleton()
	e := Start(srv.URL, "key", testTeamID)
	defer Stop()

	require.NoError(t, e.Check(EvalContext{SubscriberID: "sub1"}))
}

func TestEngine_RejectsNonHTTPBaseURL(t *testing.T) {
	// Non-http(s) schemes or schemeless strings must not cause the
	// engine to ship the API key anywhere — Start should no-op with a
	// warning, and Check should still be callable (returns nil = allowed).
	for _, url := range []string{"file:///etc/passwd", "ftp://example.com", "not-a-url", ""} {
		resetSingleton()
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

	resetSingleton()
	e := Start(srv.URL, "key", "")
	defer Stop()

	require.NotNil(t, e)
	assert.Equal(t, int32(0), atomic.LoadInt32(&hits), "no HTTP calls should be made without teamID")

	require.NoError(t, e.fetchRules(context.Background()))
	require.NoError(t, e.Check(EvalContext{SubscriberID: "sub1"}))
}

func TestEngine_PackageLevelCheckAllowsWhenDormant(t *testing.T) {
	// Get() returns nil before Start, and the package-level Check convenience
	// must tolerate that (enforcement fails open by design).
	resetSingleton()
	require.NoError(t, Check(EvalContext{SubscriberID: "sub1"}))
}

func TestEngine_ShadowRuleDoesNotBlock(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeEnvelope(w, []Rule{
			{
				RuleID: 99, Name: "shadow-test", SubscriberID: "sub1",
				Threshold: 100, CurrentValue: 500, Action: ActionBlock,
				Breached: true, ShadowMode: true,
			},
		})
	}))
	defer srv.Close()

	resetSingleton()
	e := Start(srv.URL, "key", testTeamID)
	defer Stop()

	require.NoError(t, e.Check(EvalContext{SubscriberID: "sub1"}),
		"shadowMode=true must never block even when breached and action=BLOCK")
}
