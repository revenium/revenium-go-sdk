package fal

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/revenium/revenium-go-sdk/core/enforcement"
	"github.com/revenium/revenium-go-sdk/core/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newEnforcedFal(t *testing.T, falURL, rulesURL, meteringURL, teamID string) *ReveniumFal {
	t.Helper()
	client, err := NewReveniumFal(&Config{
		FalAPIKey:      "test-key",
		FalBaseURL:     falURL,
		QueueBaseURL:   falURL,
		RequestTimeout: 10 * time.Second,
		Revenium: &core.ReveniumConfig{
			APIKey:             "hak_test_key_123",
			BaseURL:            meteringURL,
			EnforcementBaseURL: rulesURL,
			TeamID:             teamID,
		},
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func asFalSubscriber(email string) context.Context {
	return core.WithSubscriber(context.Background(), &core.Subscriber{Email: email})
}

const breachedFalPerPersonRule = `{"rules": [{"ruleId": 7, "name": "per-dev", "metricType": "TOTAL_COST", "threshold": 50,
	"currentValue": 61, "breached": true, "shadowMode": false, "action": "BLOCK", "groupBy": "SUBSCRIBER",
	"filters": [{"dimension": "PROVIDER", "operator": "IS", "value": "fal_ai"}],
	"groupBreakdown": [
		{"groupValue": "alice@example.com", "currentValue": 61, "breached": true},
		{"groupValue": "bob@example.com", "currentValue": 3, "breached": false}]}]}`

func newEnforcedFalAgainst(t *testing.T, handler http.HandlerFunc) (*ReveniumFal, *atomic.Int32) {
	t.Helper()
	enforcement.Stop()
	t.Cleanup(enforcement.Stop)
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		handler(w, r)
	}))
	t.Cleanup(server.Close)
	rules := testutil.NewMockRulesServer()
	t.Cleanup(rules.Close)
	metering := testutil.NewMockMeteringServer()
	t.Cleanup(metering.Close)
	rules.SetPayload("team-a", breachedFalPerPersonRule)
	return newEnforcedFal(t, server.URL, rules.URL(), metering.URL(), "team-a"), &hits
}

func requireBlockedByCostLimit(t *testing.T, err error, hits *atomic.Int32) {
	t.Helper()
	var ece *enforcement.ErrCostLimitExceeded
	require.True(t, errors.As(err, &ece), "alice should be blocked, got %v", err)
	assert.Equal(t, int32(0), hits.Load())
}

func TestRun_PerPersonCapBlocksOnlyThePersonOverIt(t *testing.T) {
	client, hits := newEnforcedFalAgainst(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"images":[{"url":"https://x/img.png","width":512,"height":512}]}`))
	})

	_, err := client.Run(asFalSubscriber("Alice@Example.com"), "fal-ai/flux/schnell", map[string]interface{}{"prompt": "cat"}, nil)
	requireBlockedByCostLimit(t, err, hits)

	_, err = client.Run(asFalSubscriber("bob@example.com"), "fal-ai/flux/schnell", map[string]interface{}{"prompt": "cat"}, nil)
	require.NoError(t, err)
	assert.Equal(t, int32(1), hits.Load())
}

func TestSubscribe_PerPersonCapBlocksOnlyThePersonOverIt(t *testing.T) {
	client, hits := newEnforcedFalAgainst(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost:
			_, _ = w.Write([]byte(`{"request_id":"req-1"}`))
		case strings.HasSuffix(r.URL.Path, "/requests/req-1/status"):
			_, _ = w.Write([]byte(`{"status":"COMPLETED","request_id":"req-1"}`))
		default:
			_, _ = w.Write([]byte(`{"video":{"url":"https://x/v.mp4","duration":5}}`))
		}
	})

	_, err := client.Subscribe(asFalSubscriber("Alice@Example.com"), "fal-ai/flux/schnell", map[string]interface{}{"prompt": "cat"}, nil)
	requireBlockedByCostLimit(t, err, hits)

	_, err = client.Subscribe(asFalSubscriber("bob@example.com"), "fal-ai/flux/schnell", map[string]interface{}{"prompt": "cat"}, nil)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, hits.Load(), int32(3))
}

func TestStream_PerPersonCapBlocksOnlyThePersonOverIt(t *testing.T) {
	client, hits := newEnforcedFalAgainst(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"output\":\"hi\"}\n\ndata: [DONE]\n\n"))
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	})

	_, err := client.Stream(asFalSubscriber("Alice@Example.com"), "fal-ai/openrouter/llama-3", map[string]interface{}{"prompt": "hi"}, nil)
	requireBlockedByCostLimit(t, err, hits)

	events, err := client.Stream(asFalSubscriber("bob@example.com"), "fal-ai/openrouter/llama-3", map[string]interface{}{"prompt": "hi"}, nil)
	require.NoError(t, err)
	for range events {
	}
	assert.Equal(t, int32(1), hits.Load())
}

func TestRun_MetadataArgumentSubscriberIsEnforced(t *testing.T) {
	client, hits := newEnforcedFalAgainst(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"images":[{"url":"https://x/img.png","width":512,"height":512}]}`))
	})

	aliceMeta := map[string]interface{}{"subscriber": map[string]interface{}{"email": "alice@example.com"}}
	_, err := client.Run(context.Background(), "fal-ai/flux/schnell", map[string]interface{}{"prompt": "cat"}, aliceMeta)
	requireBlockedByCostLimit(t, err, hits)

	bobMeta := map[string]interface{}{"subscriber": map[string]interface{}{"email": "bob@example.com"}}
	_, err = client.Run(context.Background(), "fal-ai/flux/schnell", map[string]interface{}{"prompt": "cat"}, bobMeta)
	require.NoError(t, err)
	assert.Equal(t, int32(1), hits.Load())
}

const breachedFalModelRule = `{"rules": [{"ruleId": 9, "name": "model-cap", "metricType": "TOTAL_COST", "threshold": 50,
	"currentValue": 61, "breached": true, "shadowMode": false, "action": "BLOCK", "groupBy": "SUBSCRIBER",
	"filters": [{"dimension": "MODEL", "operator": "IS", "value": "fal_ai/fal-ai/flux/schnell"}],
	"groupBreakdown": [{"groupValue": "alice@example.com", "currentValue": 61, "breached": true}]}]}`

const falModelRuleExceptSchnell = `{"rules": [{"ruleId": 10, "name": "all-but-schnell", "metricType": "TOTAL_COST", "threshold": 50,
	"currentValue": 61, "breached": true, "shadowMode": false, "action": "BLOCK",
	"filters": [{"dimension": "MODEL", "operator": "IS_NOT", "value": "fal_ai/fal-ai/flux/schnell"}]}]}`

func runSchnellUnderRules(t *testing.T, rulesPayload string) (*atomic.Int32, error) {
	t.Helper()
	enforcement.Stop()
	t.Cleanup(enforcement.Stop)
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"images":[{"url":"https://x/img.png","width":512,"height":512}]}`))
	}))
	t.Cleanup(server.Close)
	rules := testutil.NewMockRulesServer()
	t.Cleanup(rules.Close)
	metering := testutil.NewMockMeteringServer()
	t.Cleanup(metering.Close)
	rules.SetPayload("team-a", rulesPayload)
	client := newEnforcedFal(t, server.URL, rules.URL(), metering.URL(), "team-a")

	_, err := client.Run(asFalSubscriber("alice@example.com"), "fal-ai/flux/schnell", map[string]interface{}{"prompt": "cat"}, nil)
	return &hits, err
}

func TestRun_ModelCapMatchesNormalizedModelName(t *testing.T) {
	hits, err := runSchnellUnderRules(t, breachedFalModelRule)
	requireBlockedByCostLimit(t, err, hits)
}

func TestRun_ModelIsNotCapExemptsTheEndpointItNames(t *testing.T) {
	hits, err := runSchnellUnderRules(t, falModelRuleExceptSchnell)
	require.NoError(t, err)
	assert.Equal(t, int32(1), hits.Load())
}
