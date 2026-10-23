package runway

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/revenium/revenium-go-sdk/core/enforcement"
	"github.com/revenium/revenium-go-sdk/core/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type runwayStub struct {
	server *httptest.Server
	hits   atomic.Int32
}

func newRunwayStub(t *testing.T) *runwayStub {
	t.Helper()
	stub := &runwayStub{}
	stub.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		stub.hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		if strings.HasPrefix(r.URL.Path, "/v1/tasks/") {
			_, _ = w.Write([]byte(`{"id":"task-1","status":"SUCCEEDED","output":["https://x/out.mp4"]}`))
			return
		}
		_, _ = w.Write([]byte(`{"id":"task-1","status":"SUCCEEDED"}`))
	}))
	t.Cleanup(stub.server.Close)
	return stub
}

func newEnforcedRunway(t *testing.T, rulesURL, meteringURL, providerURL, teamID string) *ReveniumRunway {
	t.Helper()
	r, err := NewReveniumRunway(&Config{
		RunwayAPIKey:  "test-key",
		RunwayBaseURL: providerURL,
		RunwayVersion: "2024-11-06",
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

func TestVideoSurfaces_PerPersonCapBlocksOnlyThePersonOverIt(t *testing.T) {
	surfaces := []struct {
		name string
		call func(ctx context.Context, r *ReveniumRunway) error
	}{
		{"image to video", func(ctx context.Context, r *ReveniumRunway) error {
			_, err := r.ImageToVideo(ctx, &ImageToVideoRequest{PromptImage: "https://x/in.png"}, nil)
			return err
		}},
		{"video to video", func(ctx context.Context, r *ReveniumRunway) error {
			_, err := r.VideoToVideo(ctx, &VideoToVideoRequest{PromptVideo: "https://x/in.mp4"}, nil)
			return err
		}},
		{"upscale video", func(ctx context.Context, r *ReveniumRunway) error {
			_, err := r.UpscaleVideo(ctx, &VideoUpscaleRequest{PromptVideo: "https://x/in.mp4", SourceDurationSeconds: 10}, nil)
			return err
		}},
	}
	for _, surface := range surfaces {
		t.Run(surface.name, func(t *testing.T) {
			enforcement.Stop()
			t.Cleanup(enforcement.Stop)
			provider := newRunwayStub(t)
			rules := testutil.NewMockRulesServer()
			t.Cleanup(rules.Close)
			metering := testutil.NewMockMeteringServer()
			t.Cleanup(metering.Close)
			rules.SetPayload("team-a", `{"rules": [{"ruleId": 7, "name": "per-dev", "metricType": "TOTAL_COST", "threshold": 50,
				"currentValue": 61, "breached": true, "shadowMode": false, "action": "BLOCK", "groupBy": "SUBSCRIBER",
				"filters": [{"dimension": "PROVIDER", "operator": "IS", "value": "runway"}],
				"groupBreakdown": [
					{"groupValue": "alice@example.com", "currentValue": 61, "breached": true},
					{"groupValue": "bob@example.com", "currentValue": 3, "breached": false}]}]}`)

			client := newEnforcedRunway(t, rules.URL(), metering.URL(), provider.server.URL, "team-a")

			err := surface.call(asSubscriber("Alice@Example.com"), client)
			var ece *enforcement.ErrCostLimitExceeded
			require.True(t, errors.As(err, &ece), "alice should be blocked, got %v", err)
			assert.Equal(t, int32(0), provider.hits.Load())

			require.NoError(t, surface.call(asSubscriber("bob@example.com"), client))
			assert.Equal(t, int32(2), provider.hits.Load())
		})
	}
}

func TestImageToVideo_EachClientEnforcesItsOwnTeamsRules(t *testing.T) {
	enforcement.Stop()
	t.Cleanup(enforcement.Stop)
	provider := newRunwayStub(t)
	rules := testutil.NewMockRulesServer()
	t.Cleanup(rules.Close)
	metering := testutil.NewMockMeteringServer()
	t.Cleanup(metering.Close)
	rules.SetPayload("team-b", `{"rules": [{"ruleId": 4211, "threshold": 10, "currentValue": 12, "breached": true,
		"shadowMode": false, "action": "BLOCK", "filters": []}]}`)

	unblocked := newEnforcedRunway(t, rules.URL(), metering.URL(), provider.server.URL, "team-a")
	blockedTeam := newEnforcedRunway(t, rules.URL(), metering.URL(), provider.server.URL, "team-b")
	req := &ImageToVideoRequest{PromptImage: "https://x/in.png"}

	_, err := unblocked.ImageToVideo(asSubscriber("alice@example.com"), req, nil)
	require.NoError(t, err)

	_, err = blockedTeam.ImageToVideo(asSubscriber("alice@example.com"), req, nil)
	var ece *enforcement.ErrCostLimitExceeded
	require.True(t, errors.As(err, &ece), "team-b's client should be blocked, got %v", err)
	assert.Equal(t, "4211", ece.RuleID)
	assert.Equal(t, int32(2), provider.hits.Load())
}

func TestImageToVideo_MetadataArgumentSubscriberIsEnforced(t *testing.T) {
	enforcement.Stop()
	t.Cleanup(enforcement.Stop)
	provider := newRunwayStub(t)
	rules := testutil.NewMockRulesServer()
	t.Cleanup(rules.Close)
	metering := testutil.NewMockMeteringServer()
	t.Cleanup(metering.Close)
	rules.SetPayload("team-a", `{"rules": [{"ruleId": 7, "name": "per-dev", "metricType": "TOTAL_COST", "threshold": 50,
		"currentValue": 61, "breached": true, "shadowMode": false, "action": "BLOCK", "groupBy": "SUBSCRIBER",
		"filters": [{"dimension": "PROVIDER", "operator": "IS", "value": "runway"}],
		"groupBreakdown": [
			{"groupValue": "alice@example.com", "currentValue": 61, "breached": true},
			{"groupValue": "bob@example.com", "currentValue": 3, "breached": false}]}]}`)

	client := newEnforcedRunway(t, rules.URL(), metering.URL(), provider.server.URL, "team-a")
	request := &ImageToVideoRequest{PromptImage: "https://x/in.png"}

	aliceMeta := &UsageMetadata{Subscriber: map[string]interface{}{"email": "alice@example.com"}}
	_, err := client.ImageToVideo(context.Background(), request, aliceMeta)
	var ece *enforcement.ErrCostLimitExceeded
	require.True(t, errors.As(err, &ece), "alice should be blocked, got %v", err)
	assert.Equal(t, int32(0), provider.hits.Load())

	bobMeta := &UsageMetadata{Subscriber: map[string]interface{}{"email": "bob@example.com"}}
	_, err = client.ImageToVideo(context.Background(), request, bobMeta)
	require.NoError(t, err)
	assert.Equal(t, int32(2), provider.hits.Load())
}
