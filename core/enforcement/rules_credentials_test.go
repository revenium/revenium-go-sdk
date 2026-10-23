package enforcement

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const breachedTeamWideBlock = `{"rules": [{"ruleId": 7, "threshold": 10, "currentValue": 12, "breached": true, "shadowMode": false, "action": "BLOCK", "filters": []}]}`

type keyRecordingRulesServer struct {
	*httptest.Server
	mu   sync.Mutex
	keys []string
}

func newKeyRecordingRulesServer(t *testing.T, acceptedKey string) *keyRecordingRulesServer {
	t.Helper()
	s := &keyRecordingRulesServer{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.Header.Get("x-api-key")
		s.mu.Lock()
		s.keys = append(s.keys, r.URL.Path+" "+key)
		s.mu.Unlock()
		if r.URL.Path != rulesPath {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if key != acceptedKey {
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"error":"Insufficient API key scope"}`))
			return
		}
		_, _ = w.Write([]byte(breachedTeamWideBlock))
	}))
	t.Cleanup(s.Close)
	return s
}

func (s *keyRecordingRulesServer) requests() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.keys...)
}

func TestStartForFetchesRulesWithTheEnforcementKeyAndLeavesTheMeteringKeyAlone(t *testing.T) {
	rules := newKeyRecordingRulesServer(t, "rev_rk_read")

	resetEngines()
	defer Stop()
	e := StartFor(&core.ReveniumConfig{
		APIKey:            "rev_mk_metering",
		EnforcementAPIKey: "rev_rk_read",
		BaseURL:           rules.URL,
		TeamID:            testTeamID,
	})

	assert.Equal(t, []string{rulesPath + " rev_rk_read"}, rules.requests())
	assert.Error(t, e.Check(EvalContext{SubscriberEmail: "anyone@example.com"}), "the rules the read key loaded must enforce")
}

func TestStartForFallsBackToTheMeteringKeyWhenNoEnforcementKeyIsSet(t *testing.T) {
	rules := newKeyRecordingRulesServer(t, "rev_mk_metering")

	resetEngines()
	defer Stop()
	e := StartFor(&core.ReveniumConfig{APIKey: "rev_mk_metering", BaseURL: rules.URL, TeamID: testTeamID})

	assert.Equal(t, []string{rulesPath + " rev_mk_metering"}, rules.requests())
	assert.Error(t, e.Check(EvalContext{SubscriberEmail: "anyone@example.com"}))
}

func TestStartForReachesTheRulesWithOnlyTheMeteringBaseURL(t *testing.T) {
	rules := newKeyRecordingRulesServer(t, "rev_mk_metering")

	resetEngines()
	defer Stop()
	StartFor(&core.ReveniumConfig{APIKey: "rev_mk_metering", BaseURL: rules.URL, TeamID: testTeamID})

	assert.Equal(t, []string{rulesPath + " rev_mk_metering"}, rules.requests())
}

func TestTeamsWithDifferentRuleKeysPollSeparately(t *testing.T) {
	rules := newKeyRecordingRulesServer(t, "rev_rk_read")

	resetEngines()
	defer Stop()
	StartFor(&core.ReveniumConfig{APIKey: "rev_mk_metering", EnforcementAPIKey: "rev_rk_read", BaseURL: rules.URL, TeamID: testTeamID})
	StartFor(&core.ReveniumConfig{APIKey: "rev_mk_metering", BaseURL: rules.URL, TeamID: testTeamID})

	assert.ElementsMatch(t, []string{rulesPath + " rev_rk_read", rulesPath + " rev_mk_metering"}, rules.requests())
}

func fetchOnce(t *testing.T, baseURL, apiKey string) error {
	t.Helper()
	return newPoller(engineKey{baseURL: baseURL, apiKey: apiKey, teamID: testTeamID}).fetchRules(context.Background())
}

func TestRefusedRuleFetchNamesTheKeySettingToChange(t *testing.T) {
	rules := newKeyRecordingRulesServer(t, "rev_rk_read")

	err := fetchOnce(t, rules.URL, "rev_mk_metering")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "unexpected status 403")
	assert.Contains(t, err.Error(), core.EnvEnforcementAPIKey)
	assert.Contains(t, err.Error(), core.EnvTeamID)
	assert.Contains(t, err.Error(), "rev_rk_")
}

func TestUnauthorizedRuleFetchNamesTheKeySettingToChange(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	err := fetchOnce(t, srv.URL, "rev_mk_unknown")

	require.Error(t, err)
	assert.Contains(t, err.Error(), core.EnvEnforcementAPIKey)
}

func TestRuleFetchAtAWrongAddressNamesTheAddressSettingToChange(t *testing.T) {
	rules := newKeyRecordingRulesServer(t, "rev_rk_read")

	err := fetchOnce(t, rules.URL+"/somewhere-else", "rev_rk_read")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "unexpected status 404")
	assert.Contains(t, err.Error(), core.EnvEnforcementBaseURL)
	assert.Contains(t, err.Error(), core.EnvBaseURL)
	assert.NotContains(t, err.Error(), core.EnvEnforcementAPIKey)
}

func TestServerErrorOnRuleFetchCarriesNoSettingHint(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()

	err := fetchOnce(t, srv.URL, "rev_rk_read")

	require.Error(t, err)
	assert.Equal(t, "unexpected status 502: ", err.Error())
}

func TestStartFor_ReadsRulesWithTheKeyAndAddressTheReveniumConfigNames(t *testing.T) {
	t.Setenv(core.EnvEnforcementAPIKey, "")
	t.Setenv(core.EnvEnforcementBaseURL, "")
	configs := map[string]struct {
		rc       *core.ReveniumConfig
		wantKey  string
		wantAddr string
	}{
		"enforcement settings":             {&core.ReveniumConfig{APIKey: "hak_metering", EnforcementAPIKey: "rev_rk_read", BaseURL: "https://metering.example", EnforcementBaseURL: "https://rules.example/profitstream/"}, "rev_rk_read", "https://rules.example"},
		"metering settings only":           {&core.ReveniumConfig{APIKey: "hak_metering", BaseURL: "https://metering.example/meter/v2/"}, "hak_metering", "https://metering.example"},
		"enforcement address with a slash": {&core.ReveniumConfig{APIKey: "hak_metering", EnforcementBaseURL: "https://rules.example/"}, "hak_metering", "https://rules.example"},
	}
	for name, tc := range configs {
		t.Run(name, func(t *testing.T) {
			Stop()
			t.Cleanup(Stop)

			e := StartFor(tc.rc)
			t.Cleanup(e.Release)

			assert.Equal(t, tc.wantAddr, e.key.baseURL)
			assert.Equal(t, tc.wantKey, e.key.apiKey)
			assert.Equal(t, tc.wantAddr, core.RulesAddress(tc.rc.RulesBaseURL()), "Initialize and the engine agree on the address")
			assert.Equal(t, tc.wantKey, tc.rc.RulesAPIKey(), "Initialize and the engine agree on the key")
		})
	}
}
