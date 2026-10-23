package metering

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type deliveredRequest struct {
	path   string
	header http.Header
	body   map[string]interface{}
}

type flakyRevenium struct {
	server   *httptest.Server
	down     atomic.Bool
	mu       sync.Mutex
	accepted []deliveredRequest
}

func newFlakyRevenium(t *testing.T) *flakyRevenium {
	t.Helper()
	f := &flakyRevenium{}
	f.down.Store(true)
	f.server = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.server.Close)
	return f
}

func (f *flakyRevenium) serve(w http.ResponseWriter, r *http.Request) {
	if f.down.Load() {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	raw, _ := io.ReadAll(r.Body)
	var body map[string]interface{}
	_ = json.Unmarshal(raw, &body)
	f.mu.Lock()
	f.accepted = append(f.accepted, deliveredRequest{path: r.URL.Path, header: r.Header.Clone(), body: body})
	f.mu.Unlock()
	w.WriteHeader(http.StatusOK)
}

func (f *flakyRevenium) onlyAccepted(t *testing.T) deliveredRequest {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	require.Len(t, f.accepted, 1)
	return f.accepted[0]
}

func TestClient_ReplaysAHeldUsageRecordWithItsKeyAndBody(t *testing.T) {
	revenium := newFlakyRevenium(t)
	client := newTestClient(t, revenium.server.URL)
	t.Cleanup(func() { _ = client.Close() })

	payload := NewPayload(OperationChat, "gpt-4.1-nano", "OPENAI").
		WithTokens(100, 20, 120).
		WithReasoningTokens(5, 7, 9).
		WithStopReason("TOKEN_LIMIT").
		WithIdempotencyKey("held-record-1").
		Build()
	payload.Agent = "agent-1"
	_ = client.SendSync(payload)
	require.Equal(t, 1, client.GetBufferStats().Size)

	revenium.down.Store(false)
	client.Flush()

	replayed := revenium.onlyAccepted(t)
	assert.Equal(t, "/meter/v2/ai/completions", replayed.path)
	assert.Equal(t, "test-key", replayed.header.Get("x-api-key"))
	assert.Equal(t, "application/json; charset=utf-8", replayed.header.Get("Content-Type"))
	assert.Equal(t, "held-record-1", replayed.header.Get("Idempotency-Key"))
	assert.Equal(t, "gpt-4.1-nano", replayed.body["model"])
	assert.Equal(t, "OPENAI", replayed.body["provider"])
	assert.Equal(t, float64(100), replayed.body["inputTokenCount"])
	assert.Equal(t, float64(20), replayed.body["outputTokenCount"])
	assert.Equal(t, float64(120), replayed.body["totalTokenCount"])
	assert.Equal(t, float64(5), replayed.body["reasoningTokenCount"])
	assert.Equal(t, float64(7), replayed.body["cacheCreationTokenCount"])
	assert.Equal(t, float64(9), replayed.body["cacheReadTokenCount"])
	assert.Equal(t, "TOKEN_LIMIT", replayed.body["stopReason"])
	assert.Equal(t, "agent-1", replayed.body["agent"])
}

func TestClient_ReplaysAHeldToolEventWithItsKeyAndBody(t *testing.T) {
	revenium := newFlakyRevenium(t)
	client := newTestClient(t, revenium.server.URL)
	t.Cleanup(func() { _ = client.Close() })

	_ = client.SendToolEventSync(&ToolEventPayload{IdempotencyKey: "held-tool-1", ToolID: "web-search"})
	require.Equal(t, 1, client.GetBufferStats().Size)

	revenium.down.Store(false)
	client.Flush()

	replayed := revenium.onlyAccepted(t)
	assert.Equal(t, "/meter/v2/tool/events", replayed.path)
	assert.Equal(t, "test-key", replayed.header.Get("x-api-key"))
	assert.Equal(t, "held-tool-1", replayed.header.Get("Idempotency-Key"))
	require.NotNil(t, replayed.body)
	assert.Equal(t, "web-search", replayed.body["toolId"])
}
