package openai

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/revenium/revenium-go-sdk/core/enforcement"
	"github.com/revenium/revenium-go-sdk/core/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const aliceBlockedByRule4211 = `{"rules": [{"ruleId": 4211, "name": "alice cap", "threshold": 10, "currentValue": 12,
	"periodType": "MONTHLY", "breached": true, "shadowMode": false, "action": "BLOCK",
	"filters": [{"dimension": "SUBSCRIBER", "operator": "IS", "value": "alice@example.com"}]}]}`

func TestChatCompletions_EachClientEnforcesItsOwnTeamsRules(t *testing.T) {
	cases := []struct {
		name      string
		teamA     string
		teamB     string
		payloads  map[string]string
		bBlocked  bool
		fetchedBy []string
	}{
		{
			name:      "second team's breached cap applies to its own client",
			teamA:     "teamA",
			teamB:     "teamB",
			payloads:  map[string]string{"teamB": aliceBlockedByRule4211},
			bBlocked:  true,
			fetchedBy: []string{"teamA", "teamB"},
		},
		{
			name:      "first team's breached cap does not leak into another team's client",
			teamA:     "teamC",
			teamB:     "teamD",
			payloads:  map[string]string{"teamC": aliceBlockedByRule4211},
			bBlocked:  false,
			fetchedBy: []string{"teamC", "teamD"},
		},
		{
			name:      "a first client without a team does not switch off later teams",
			teamA:     "",
			teamB:     "teamB",
			payloads:  map[string]string{"teamB": aliceBlockedByRule4211},
			bBlocked:  true,
			fetchedBy: []string{"teamB"},
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
			for team, payload := range tc.payloads {
				rules.SetPayload(team, payload)
			}

			clientA := newEnforcedOpenAI(t, provider.server.URL, rules.URL(), metering.URL(), tc.teamA)
			clientB := newEnforcedOpenAI(t, provider.server.URL, rules.URL(), metering.URL(), tc.teamB)

			_, errA := clientA.Chat().Completions().New(asSubscriber("alice@example.com"), chatParams("gpt-4.1-nano"))
			_, errB := clientB.Chat().Completions().New(asSubscriber("alice@example.com"), chatParams("gpt-4.1-nano"))

			_, aHasRules := tc.payloads[tc.teamA]
			var ece *enforcement.ErrCostLimitExceeded
			assert.Equal(t, aHasRules, errors.As(errA, &ece), "client A verdict, err=%v", errA)
			if tc.bBlocked {
				require.True(t, errors.As(errB, &ece), "client B should be blocked, got %v", errB)
				assert.Equal(t, "4211", ece.RuleID)
			} else {
				assert.NoError(t, errB, "client B must not be blocked by another team's rule")
			}
			for _, team := range tc.fetchedBy {
				assert.Equal(t, 1, rules.Fetches(team), "rules fetched for %s", team)
			}
		})
	}
}

func TestChatCompletions_ClientsWithTheSameTeamShareOneRuleFetch(t *testing.T) {
	enforcement.Stop()
	t.Cleanup(enforcement.Stop)
	provider := newProviderStub(t)
	rules := testutil.NewMockRulesServer()
	t.Cleanup(rules.Close)
	metering := testutil.NewMockMeteringServer()
	t.Cleanup(metering.Close)
	rules.SetPayload("teamA", aliceBlockedByRule4211)

	for i := 0; i < 3; i++ {
		client := newEnforcedOpenAI(t, provider.server.URL, rules.URL(), metering.URL(), "teamA")
		_, err := client.Chat().Completions().New(asSubscriber("alice@example.com"), chatParams("gpt-4.1-nano"))
		var ece *enforcement.ErrCostLimitExceeded
		require.True(t, errors.As(err, &ece))
	}
	assert.Equal(t, 1, rules.Fetches("teamA"))
}

func TestChatCompletions_ClientFollowsEnforcementStopAndRestart(t *testing.T) {
	enforcement.Stop()
	t.Cleanup(enforcement.Stop)
	provider := newProviderStub(t)
	rules := testutil.NewMockRulesServer()
	t.Cleanup(rules.Close)
	metering := testutil.NewMockMeteringServer()
	t.Cleanup(metering.Close)
	rules.SetPayload("teamA", aliceBlockedByRule4211)

	client := newEnforcedOpenAI(t, provider.server.URL, rules.URL(), metering.URL(), "teamA")
	completions := client.Chat().Completions()
	alice := asSubscriber("alice@example.com")

	_, err := completions.New(alice, chatParams("gpt-4.1-nano"))
	var ece *enforcement.ErrCostLimitExceeded
	require.True(t, errors.As(err, &ece))

	enforcement.Stop()
	_, err = completions.New(alice, chatParams("gpt-4.1-nano"))
	require.NoError(t, err, "a client must not keep blocking on rules from a stopped engine")
	assert.Equal(t, int32(1), provider.hits.Load())

	rules.SetPayload("teamA", `{"rules": [{"ruleId": 5000, "threshold": 1, "currentValue": 2, "breached": true,
		"shadowMode": false, "action": "BLOCK", "filters": []}]}`)
	newEnforcedOpenAI(t, provider.server.URL, rules.URL(), metering.URL(), "teamA")

	_, err = completions.New(alice, chatParams("gpt-4.1-nano"))
	require.True(t, errors.As(err, &ece), "the existing client should see the restarted engine's rules, got %v", err)
	assert.Equal(t, "5000", ece.RuleID)
}

func TestChatCompletions_ClosingClientsReleasesTheirTeamsEngine(t *testing.T) {
	enforcement.Stop()
	t.Cleanup(enforcement.Stop)
	provider := newProviderStub(t)
	rules := testutil.NewMockRulesServer()
	t.Cleanup(rules.Close)
	metering := testutil.NewMockMeteringServer()
	t.Cleanup(metering.Close)
	rules.SetPayload("teamA", aliceBlockedByRule4211)

	first := newEnforcedOpenAI(t, provider.server.URL, rules.URL(), metering.URL(), "teamA")
	second := newEnforcedOpenAI(t, provider.server.URL, rules.URL(), metering.URL(), "teamA")
	alice := asSubscriber("alice@example.com")
	var ece *enforcement.ErrCostLimitExceeded

	require.NoError(t, first.Close())
	_, err := second.Chat().Completions().New(alice, chatParams("gpt-4.1-nano"))
	require.True(t, errors.As(err, &ece), "the client still open keeps its team's rules, got %v", err)
	third := newEnforcedOpenAI(t, provider.server.URL, rules.URL(), metering.URL(), "teamA")
	assert.Equal(t, 1, rules.Fetches("teamA"), "a team with an open client keeps its one poll")

	require.NoError(t, third.Close())
	require.NoError(t, second.Close())
	reopened := newEnforcedOpenAI(t, provider.server.URL, rules.URL(), metering.URL(), "teamA")
	assert.Equal(t, 2, rules.Fetches("teamA"), "once the last client closes, the team's engine is dropped")
	_, err = reopened.Chat().Completions().New(alice, chatParams("gpt-4.1-nano"))
	require.True(t, errors.As(err, &ece))
}

func TestInitialize_ACallArrivingMidSetupNeverBuildsAClientOrTakesARulesLease(t *testing.T) {
	enforcement.Stop()
	t.Cleanup(enforcement.Stop)
	t.Cleanup(func() { _ = sharedClient.ResetAndClose() })
	previousLogger := core.GetLogger()
	setups := &setupCountingLogger{Logger: previousLogger}
	core.SetLogger(setups)
	t.Cleanup(func() { core.SetLogger(previousLogger) })

	provider := newProviderStub(t)
	metering := testutil.NewMockMeteringServer()
	t.Cleanup(metering.Close)
	var fetches atomic.Int32
	fetchStarted := make(chan struct{})
	releaseFetch := make(chan struct{})
	rulesServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if fetches.Add(1) == 1 {
			close(fetchStarted)
			<-releaseFetch
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(rulesServer.Close)
	t.Setenv("REVENIUM_ENFORCEMENT_BASE_URL", rulesServer.URL)
	t.Setenv("REVENIUM_TEAM_ID", "team-init-race")

	initialize := func() error {
		return Initialize(
			WithOpenAIAPIKey("sk-test"),
			WithBaseURL(provider.server.URL),
			WithAzureDisabled(true),
			WithReveniumAPIKey("hak_test_key_123"),
			WithReveniumBaseURL(metering.URL()),
		)
	}

	first := make(chan error, 1)
	go func() { first <- initialize() }()
	<-fetchStarted

	second := make(chan error, 1)
	go func() { second <- initialize() }()

	probe := make(chan bool, 1)
	go func() { probe <- IsInitialized() }()
	select {
	case installed := <-probe:
		assert.False(t, installed, "the first setup is still fetching rules")
	case <-time.After(2 * time.Second):
		close(releaseFetch)
		<-first
		<-second
		t.Fatal("IsInitialized must not block while the initial rule fetch is in flight")
	}

	close(releaseFetch)
	require.NoError(t, <-first)
	require.NoError(t, <-second)
	require.Equal(t, int32(1), fetches.Load(), "the call that arrived mid-setup waited for it and never fetched rules itself")
	assert.Equal(t, int32(1), setups.started.Load(), "only the first call built a client; the second never started a setup of its own")

	client, err := GetClient()
	require.NoError(t, err)
	require.NoError(t, client.Close())

	newEnforcedOpenAI(t, provider.server.URL, rulesServer.URL, metering.URL(), "team-init-race")
	assert.Equal(t, int32(2), fetches.Load(),
		"closing the one installed client drops the team's poll, so the second call never took a lease of its own")
}

type setupCountingLogger struct {
	core.Logger
	started atomic.Int32
}

func (l *setupCountingLogger) Info(message string, args ...interface{}) {
	if message == "Initializing Revenium middleware..." {
		l.started.Add(1)
	}
	l.Logger.Info(message, args...)
}
