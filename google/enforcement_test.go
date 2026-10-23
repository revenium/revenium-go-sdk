package google

import (
	"context"
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
	"google.golang.org/genai"
)

type geminiStub struct {
	server *httptest.Server
	hits   atomic.Int32
}

func newGeminiStub(t *testing.T) *geminiStub {
	t.Helper()
	stub := &geminiStub{}
	stub.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		stub.hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"candidates":[{"content":{"role":"model","parts":[{"text":"hi"}]},"finishReason":"STOP"}],` +
			`"usageMetadata":{"promptTokenCount":1,"candidatesTokenCount":1,"totalTokenCount":2}}`))
	}))
	t.Cleanup(stub.server.Close)
	t.Setenv("GOOGLE_GEMINI_BASE_URL", stub.server.URL)
	return stub
}

func newEnforcedGoogle(t *testing.T, rulesURL, meteringURL, teamID string) *ReveniumGoogle {
	t.Helper()
	r, err := NewReveniumGoogle(&Config{
		GoogleAPIKey: "test-key",
		Revenium: &core.ReveniumConfig{
			APIKey:             "hak_test_key_123",
			BaseURL:            meteringURL,
			EnforcementBaseURL: rulesURL,
			TeamID:             teamID,
		},
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = r.Close() })
	return r
}

func asSubscriber(email string) context.Context {
	return core.WithSubscriber(context.Background(), &core.Subscriber{Email: email})
}

func TestGenerateContent_PerPersonCapBlocksOnlyThePersonOverIt(t *testing.T) {
	enforcement.Stop()
	t.Cleanup(enforcement.Stop)
	provider := newGeminiStub(t)
	rules := testutil.NewMockRulesServer()
	t.Cleanup(rules.Close)
	metering := testutil.NewMockMeteringServer()
	t.Cleanup(metering.Close)
	rules.SetPayload("team-a", `{"rules": [{"ruleId": 7, "name": "per-dev", "metricType": "TOTAL_COST", "threshold": 50,
		"currentValue": 61, "breached": true, "shadowMode": false, "action": "BLOCK", "groupBy": "SUBSCRIBER",
		"filters": [{"dimension": "PROVIDER", "operator": "IS", "value": "GOOGLE_AI"}],
		"groupBreakdown": [
			{"groupValue": "alice@example.com", "currentValue": 61, "breached": true},
			{"groupValue": "bob@example.com", "currentValue": 3, "breached": false}]}]}`)

	models := newEnforcedGoogle(t, rules.URL(), metering.URL(), "team-a").Models()
	contents := genai.Text("hi")

	_, err := models.GenerateContent(asSubscriber("Alice@Example.com"), "gemini-2.0-flash", contents, nil)
	var ece *enforcement.ErrCostLimitExceeded
	require.True(t, errors.As(err, &ece), "alice should be blocked, got %v", err)
	assert.Equal(t, int32(0), provider.hits.Load())

	_, err = models.GenerateContent(asSubscriber("bob@example.com"), "gemini-2.0-flash", contents, nil)
	require.NoError(t, err)
	assert.Equal(t, int32(1), provider.hits.Load())

	for _, streamErr := range models.GenerateContentStream(asSubscriber("alice@example.com"), "gemini-2.0-flash", contents, nil) {
		assert.True(t, errors.As(streamErr, &ece), "alice's stream should be blocked, got %v", streamErr)
		break
	}
	assert.Equal(t, int32(1), provider.hits.Load())
}

func TestGenerateContent_EachClientEnforcesItsOwnTeamsRules(t *testing.T) {
	enforcement.Stop()
	t.Cleanup(enforcement.Stop)
	provider := newGeminiStub(t)
	rules := testutil.NewMockRulesServer()
	t.Cleanup(rules.Close)
	metering := testutil.NewMockMeteringServer()
	t.Cleanup(metering.Close)
	rules.SetPayload("team-b", `{"rules": [{"ruleId": 4211, "threshold": 10, "currentValue": 12, "breached": true,
		"shadowMode": false, "action": "BLOCK", "filters": []}]}`)

	unblocked := newEnforcedGoogle(t, rules.URL(), metering.URL(), "team-a").Models()
	blockedTeam := newEnforcedGoogle(t, rules.URL(), metering.URL(), "team-b").Models()
	contents := genai.Text("hi")

	_, err := unblocked.GenerateContent(asSubscriber("alice@example.com"), "gemini-2.0-flash", contents, nil)
	require.NoError(t, err)

	_, err = blockedTeam.GenerateContent(asSubscriber("alice@example.com"), "gemini-2.0-flash", contents, nil)
	var ece *enforcement.ErrCostLimitExceeded
	require.True(t, errors.As(err, &ece), "team-b's client should be blocked, got %v", err)
	assert.Equal(t, "4211", ece.RuleID)
	assert.Equal(t, int32(1), provider.hits.Load())
}

func TestInitialize_RacingLoserReleasesItsEnforcementLease(t *testing.T) {
	enforcement.Stop()
	t.Cleanup(enforcement.Stop)
	t.Cleanup(Reset)

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
	t.Setenv("GOOGLE_CLOUD_PROJECT", "")

	initialize := func() error {
		return Initialize(
			WithGoogleAPIKey("test-key"),
			WithReveniumAPIKey("hak_test_key_123"),
			WithReveniumBaseURL(metering.URL()),
		)
	}

	winner := make(chan error, 1)
	go func() { winner <- initialize() }()
	<-fetchStarted

	loser := make(chan error, 1)
	go func() { loser <- initialize() }()

	probe := make(chan bool, 1)
	go func() { probe <- IsInitialized() }()
	select {
	case installed := <-probe:
		assert.False(t, installed, "the winner is still fetching rules")
	case <-time.After(2 * time.Second):
		close(releaseFetch)
		<-winner
		<-loser
		t.Fatal("IsInitialized must not block while the initial rule fetch is in flight")
	}

	close(releaseFetch)
	require.NoError(t, <-winner)
	require.NoError(t, <-loser)
	require.Equal(t, int32(1), fetches.Load(), "racing clients share one rule fetch")

	Reset()

	newEnforcedGoogle(t, rulesServer.URL, metering.URL(), "team-init-race")
	assert.Equal(t, int32(2), fetches.Load(),
		"resetting the installed client must drop the team's poll; a leaked loser lease keeps it alive")
}

func TestGenerateVideo_PerPersonCapBlocksBeforeSubmission(t *testing.T) {
	enforcement.Stop()
	t.Cleanup(enforcement.Stop)
	provider := newGeminiStub(t)
	rules := testutil.NewMockRulesServer()
	t.Cleanup(rules.Close)
	metering := testutil.NewMockMeteringServer()
	t.Cleanup(metering.Close)
	rules.SetPayload("team-a", `{"rules": [{"ruleId": 7, "name": "per-dev", "metricType": "TOTAL_COST", "threshold": 50,
		"currentValue": 61, "breached": true, "shadowMode": false, "action": "BLOCK", "groupBy": "SUBSCRIBER",
		"filters": [{"dimension": "PROVIDER", "operator": "IS", "value": "GOOGLE_AI"}],
		"groupBreakdown": [
			{"groupValue": "alice@example.com", "currentValue": 61, "breached": true},
			{"groupValue": "bob@example.com", "currentValue": 3, "breached": false}]}]}`)

	models := newEnforcedGoogle(t, rules.URL(), metering.URL(), "team-a").Models()
	var ece *enforcement.ErrCostLimitExceeded

	_, err := models.GenerateVideo(asSubscriber("alice@example.com"), "veo-3.0-generate-001", "a sunrise", nil, nil)
	require.True(t, errors.As(err, &ece), "alice's video should be blocked, got %v", err)
	_, err = models.GenerateVideoFromSource(asSubscriber("alice@example.com"), "veo-3.0-generate-001", &genai.GenerateVideosSource{Prompt: "a sunrise"}, nil)
	require.True(t, errors.As(err, &ece), "alice's video from source should be blocked, got %v", err)
	assert.Equal(t, int32(0), provider.hits.Load(), "a blocked video never reaches Google")

	_, _ = models.GenerateVideo(asSubscriber("bob@example.com"), "veo-3.0-generate-001", "a sunrise", nil, nil)
	assert.Equal(t, int32(1), provider.hits.Load(), "an unblocked video is submitted")
}

func TestEmbeddingAndImageCalls_PerPersonCapBlocksBeforeTheRequest(t *testing.T) {
	calls := map[string]func(m *ModelsInterface, ctx context.Context) error{
		"CreateEmbedding": func(m *ModelsInterface, ctx context.Context) error {
			_, err := m.CreateEmbedding(ctx, "text-embedding-005", genai.Text("hi"), nil)
			return err
		},
		"GenerateImage": func(m *ModelsInterface, ctx context.Context) error {
			_, err := m.GenerateImage(ctx, "imagen-4.0-generate-001", "a sunrise", nil)
			return err
		},
		"EditImage": func(m *ModelsInterface, ctx context.Context) error {
			_, err := m.EditImage(ctx, "imagen-3.0-capability-001", "add a boat", nil, nil)
			return err
		},
		"UpscaleImage": func(m *ModelsInterface, ctx context.Context) error {
			_, err := m.UpscaleImage(ctx, "imagen-4.0-upscale-preview", &genai.Image{ImageBytes: []byte("png"), MIMEType: "image/png"}, "x2", nil)
			return err
		},
	}
	for name, call := range calls {
		t.Run(name, func(t *testing.T) {
			enforcement.Stop()
			t.Cleanup(enforcement.Stop)
			var hits atomic.Int32
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				hits.Add(1)
				writeJSON(w, http.StatusOK, `{}`)
			}))
			t.Cleanup(provider.Close)
			rules := testutil.NewMockRulesServer()
			t.Cleanup(rules.Close)
			metering := testutil.NewMockMeteringServer()
			t.Cleanup(metering.Close)
			rules.SetPayload("team-a", `{"rules": [{"ruleId": 7, "name": "per-dev", "metricType": "TOTAL_COST", "threshold": 50,
				"currentValue": 61, "breached": true, "shadowMode": false, "action": "BLOCK", "groupBy": "SUBSCRIBER",
				"filters": [{"dimension": "PROVIDER", "operator": "IS", "value": "VERTEX_AI"}],
				"groupBreakdown": [
					{"groupValue": "alice@example.com", "currentValue": 61, "breached": true},
					{"groupValue": "bob@example.com", "currentValue": 3, "breached": false}]}]}`)

			models := newClientAgainst(t, ProviderVertexAI, provider.URL, &core.ReveniumConfig{
				APIKey:             "hak_test_key_123",
				BaseURL:            metering.URL(),
				EnforcementBaseURL: rules.URL(),
				TeamID:             "team-a",
			}).Models()
			var ece *enforcement.ErrCostLimitExceeded

			err := call(models, asSubscriber("alice@example.com"))
			require.True(t, errors.As(err, &ece), "alice's %s should be blocked, got %v", name, err)
			assert.Equal(t, int32(0), hits.Load(), "a blocked call never reaches Google")

			err = call(models, asSubscriber("bob@example.com"))
			assert.False(t, errors.As(err, &ece), "bob is under the cap, got %v", err)
			assert.Equal(t, int32(1), hits.Load(), "an unblocked call is sent to Google")
		})
	}
}
