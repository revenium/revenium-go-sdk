package metering

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/revenium/revenium-go-sdk/core/resilience"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type storedToolEvent struct {
	idempotencyKey string
	body           map[string]interface{}
}

// toolEventStore answers like Revenium's tool-event endpoint: a second event
// with a transactionId or an Idempotency-Key it already holds is refused with
// 409.
type toolEventStore struct {
	mu     sync.Mutex
	stored map[string]storedToolEvent
	keys   map[string]bool
	order  []string
}

func newToolEventStore(t *testing.T) (*toolEventStore, *httptest.Server) {
	t.Helper()
	store := &toolEventStore{stored: map[string]storedToolEvent{}, keys: map[string]bool{}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		assert.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		id, _ := body["transactionId"].(string)
		store.mu.Lock()
		defer store.mu.Unlock()
		key := r.Header.Get("Idempotency-Key")
		if _, seen := store.stored[id]; seen || store.keys[key] {
			w.WriteHeader(http.StatusConflict)
			return
		}
		store.keys[key] = true
		store.stored[id] = storedToolEvent{idempotencyKey: key, body: body}
		store.order = append(store.order, id)
		w.WriteHeader(http.StatusCreated)
	}))
	t.Cleanup(srv.Close)
	return store, srv
}

func (s *toolEventStore) events() []storedToolEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	events := make([]storedToolEvent, 0, len(s.order))
	for _, id := range s.order {
		events = append(events, s.stored[id])
	}
	return events
}

func toolEventWithMetadata(metadata map[string]interface{}) *ToolEventPayload {
	payload := NewToolEvent("web_search").Build()
	ApplyToolEventMetadata(payload, metadata)
	return payload
}

func TestToolEvents_SharingACallerTransactionIDAreAllStored(t *testing.T) {
	store, srv := newToolEventStore(t)
	client := newTestClient(t, srv.URL)
	metadata := map[string]interface{}{"transactionId": "checkout-42"}

	require.NoError(t, client.SendToolEventSync(toolEventWithMetadata(metadata)))
	require.NoError(t, client.SendToolEventSync(toolEventWithMetadata(metadata)))

	events := store.events()
	require.Len(t, events, 2, "every tool call is its own event")
	for _, event := range events {
		assert.Equal(t, "checkout-42", event.body["traceId"], "the caller's id groups the events as their trace")
		assert.Equal(t, event.body["transactionId"], event.idempotencyKey)
		assert.NotEqual(t, "checkout-42", event.body["transactionId"])
	}
}

func TestToolEvent_CallerTransactionIDMovesToUsageMetadataWhenATraceIDIsSet(t *testing.T) {
	callerUsageMetadata := map[string]interface{}{"step": "lookup"}

	payload := toolEventWithMetadata(map[string]interface{}{
		"transactionId": "checkout-42",
		"traceId":       "trace-7",
		"usageMetadata": callerUsageMetadata,
	})

	assert.Equal(t, "trace-7", payload.TraceID)
	assert.Equal(t, map[string]interface{}{"step": "lookup", "callerTransactionId": "checkout-42"}, payload.UsageMetadata)
	assert.Equal(t, map[string]interface{}{"step": "lookup"}, callerUsageMetadata, "the caller's map is not modified")
	assert.NotEqual(t, "checkout-42", payload.TransactionID)
}

func TestToolEvent_KeepsACallerTransactionIDTheCallerAlreadyPutInUsageMetadata(t *testing.T) {
	payload := toolEventWithMetadata(map[string]interface{}{
		"transactionId": "checkout-42",
		"traceId":       "trace-7",
		"usageMetadata": map[string]interface{}{"callerTransactionId": "set-by-caller"},
	})

	assert.Equal(t, "set-by-caller", payload.UsageMetadata["callerTransactionId"])
}

func TestToolEvent_RetryKeepsTheEventsTransactionIDAndKey(t *testing.T) {
	resilience.ResetCircuitBreakers()
	defer resilience.ResetCircuitBreakers()
	type attempt struct{ transactionID, key string }
	var mu sync.Mutex
	var attempts []attempt
	var count atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		assert.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		id, _ := body["transactionId"].(string)
		mu.Lock()
		attempts = append(attempts, attempt{id, r.Header.Get("Idempotency-Key")})
		mu.Unlock()
		if count.Add(1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	require.NoError(t, newTestClient(t, srv.URL).SendToolEventSync(toolEventWithMetadata(map[string]interface{}{"transactionId": "checkout-42"})))

	require.Len(t, attempts, 2)
	assert.Equal(t, attempts[0], attempts[1])
	assert.Equal(t, attempts[0].transactionID, attempts[0].key)
}

func TestToolEventBuilder_IdempotencyKeyFollowsAnExplicitTransactionID(t *testing.T) {
	followed := NewToolEvent("tool").WithTransactionID("call-1").Build()
	overridden := NewToolEvent("tool").WithIdempotencyKey("key-1").WithTransactionID("call-1").Build()

	assert.Equal(t, "call-1", followed.IdempotencyKey)
	assert.Equal(t, "key-1", overridden.IdempotencyKey)
}

func TestToolEvents_SharingAnIdempotencyKeyInMetadataAreAllStored(t *testing.T) {
	store, srv := newToolEventStore(t)
	client := newTestClient(t, srv.URL)
	metadata := map[string]interface{}{"idempotencyKey": "shared-key"}

	require.NoError(t, client.SendToolEventSync(toolEventWithMetadata(metadata)))
	require.NoError(t, client.SendToolEventSync(toolEventWithMetadata(metadata)))

	events := store.events()
	require.Len(t, events, 2)
	for _, event := range events {
		assert.Equal(t, event.body["transactionId"], event.idempotencyKey)
	}
}

func TestToolEvent_ABlankTraceIDStillTakesTheCallerTransactionID(t *testing.T) {
	payload := NewToolEvent("web_search").Build()
	payload.TraceID = "  "
	ApplyToolEventMetadata(payload, map[string]interface{}{"transactionId": "checkout-42"})

	assert.Equal(t, "checkout-42", payload.TraceID)
	assert.NotContains(t, payload.UsageMetadata, "callerTransactionId")
}
