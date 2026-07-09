package metering

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/revenium/revenium-go-sdk/core/resilience"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestBuffer(maxSize int) *MeteringBuffer {
	return NewMeteringBuffer(maxSize, 1*time.Hour, 5*time.Second)
}

func makeEvent(key string) BufferedEvent {
	return BufferedEvent{
		URL:            "http://localhost/test",
		Headers:        map[string]string{"Content-Type": "application/json"},
		Body:           []byte(`{"test":true}`),
		IdempotencyKey: key,
	}
}

func TestBuffer_Push_And_Stats(t *testing.T) {
	buf := newTestBuffer(10)
	defer buf.Stop()

	buf.Push(makeEvent("k1"))
	buf.Push(makeEvent("k2"))

	stats := buf.GetBufferStats()
	assert.Equal(t, 2, stats.Size)
	assert.Equal(t, 10, stats.Capacity)
}

func TestBuffer_FIFO_Eviction(t *testing.T) {
	buf := newTestBuffer(3)
	defer buf.Stop()

	buf.Push(makeEvent("k1"))
	buf.Push(makeEvent("k2"))
	buf.Push(makeEvent("k3"))
	buf.Push(makeEvent("k4"))

	stats := buf.GetBufferStats()
	assert.Equal(t, 3, stats.Size)
	assert.Equal(t, int64(1), stats.EventsEvicted)

	buf.mu.Lock()
	assert.Equal(t, "k2", buf.events[0].IdempotencyKey)
	buf.mu.Unlock()
}

func TestBuffer_Flush_Replays_Successfully(t *testing.T) {
	var received atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received.Add(1)
		w.WriteHeader(200)
	}))
	defer srv.Close()

	buf := newTestBuffer(100)

	buf.Push(BufferedEvent{
		URL:            srv.URL,
		Headers:        map[string]string{"Content-Type": "application/json"},
		Body:           []byte(`{}`),
		IdempotencyKey: "k1",
	})
	buf.Push(BufferedEvent{
		URL:            srv.URL,
		Headers:        map[string]string{"Content-Type": "application/json"},
		Body:           []byte(`{}`),
		IdempotencyKey: "k2",
	})

	buf.flush()

	assert.Equal(t, int32(2), received.Load())
	stats := buf.GetBufferStats()
	assert.Equal(t, 0, stats.Size)
	assert.Equal(t, int64(2), stats.EventsReplayed)
}

func TestBuffer_Flush_StopsOnRetryableFailure(t *testing.T) {
	var received atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := received.Add(1)
		if count == 2 {
			w.WriteHeader(500)
			return
		}
		w.WriteHeader(200)
	}))
	defer srv.Close()

	buf := newTestBuffer(100)

	for i := 0; i < 4; i++ {
		buf.Push(BufferedEvent{
			URL:            srv.URL,
			Headers:        map[string]string{"Content-Type": "application/json"},
			Body:           []byte(`{}`),
			IdempotencyKey: "k" + string(rune('0'+i)),
		})
	}

	buf.flush()

	stats := buf.GetBufferStats()
	assert.Equal(t, int64(1), stats.EventsReplayed)
	assert.Equal(t, 3, stats.Size)
}

func TestBuffer_Flush_DiscardsNonRetryable(t *testing.T) {
	var received atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := received.Add(1)
		if count == 1 {
			w.WriteHeader(401)
			return
		}
		w.WriteHeader(200)
	}))
	defer srv.Close()

	buf := newTestBuffer(100)
	for i := 0; i < 2; i++ {
		buf.Push(BufferedEvent{
			URL:            srv.URL,
			Headers:        map[string]string{"Content-Type": "application/json"},
			Body:           []byte(`{}`),
			IdempotencyKey: "k" + string(rune('0'+i)),
		})
	}

	buf.flush()

	stats := buf.GetBufferStats()
	assert.Equal(t, 0, stats.Size)
	assert.Equal(t, int64(1), stats.EventsReplayed)
	assert.Equal(t, int64(1), stats.EventsEvicted)
}

func TestBuffer_EvictsExpiredEvents(t *testing.T) {
	buf := newTestBuffer(100)

	buf.mu.Lock()
	buf.events = append(buf.events, BufferedEvent{
		URL:            "http://localhost/test",
		Headers:        map[string]string{},
		Body:           []byte(`{}`),
		IdempotencyKey: "old",
		CreatedAt:      time.Now().Add(-25 * time.Hour),
	})
	buf.events = append(buf.events, BufferedEvent{
		URL:            "http://localhost/test",
		Headers:        map[string]string{},
		Body:           []byte(`{}`),
		IdempotencyKey: "fresh",
		CreatedAt:      time.Now(),
	})
	buf.mu.Unlock()

	buf.mu.Lock()
	buf.evictExpired()
	assert.Equal(t, 1, len(buf.events))
	assert.Equal(t, "fresh", buf.events[0].IdempotencyKey)
	buf.mu.Unlock()
}

func TestBuffer_ReplayPreservesIdempotencyKey(t *testing.T) {
	var receivedKey string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedKey = r.Header.Get("Idempotency-Key")
		w.WriteHeader(200)
	}))
	defer srv.Close()

	buf := newTestBuffer(100)
	buf.Push(BufferedEvent{
		URL: srv.URL,
		Headers: map[string]string{
			"Content-Type":    "application/json",
			"Idempotency-Key": "original-key-123",
		},
		Body:           []byte(`{}`),
		IdempotencyKey: "original-key-123",
	})

	buf.flush()

	assert.Equal(t, "original-key-123", receivedKey)
}

func TestBuffer_DrainWithTimeout(t *testing.T) {
	var received atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received.Add(1)
		w.WriteHeader(200)
	}))
	defer srv.Close()

	buf := newTestBuffer(100)
	buf.Push(BufferedEvent{
		URL:            srv.URL,
		Headers:        map[string]string{"Content-Type": "application/json"},
		Body:           []byte(`{}`),
		IdempotencyKey: "drain-test",
	})

	buf.DrainWithTimeout(5 * time.Second)

	assert.Equal(t, int32(1), received.Load())
	assert.Equal(t, 0, buf.GetBufferStats().Size)
}

func TestClient_BuffersMeteringEventAfterRetryExhaustion(t *testing.T) {
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.WriteHeader(500)
	}))
	defer srv.Close()

	client := newTestClient(t, srv.URL)

	payload := NewPayload(OperationChat, "gpt-4", "openai").
		WithTokens(10, 20, 30).
		Build()

	_ = client.SendSync(payload)

	stats := client.GetBufferStats()
	assert.Equal(t, 1, stats.Size)
}

func TestClient_DoesNotBufferNonRetryableError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
	}))
	defer srv.Close()

	client := newTestClient(t, srv.URL)

	payload := NewPayload(OperationChat, "gpt-4", "openai").
		WithTokens(10, 20, 30).
		Build()

	_ = client.SendSync(payload)

	stats := client.GetBufferStats()
	assert.Equal(t, 0, stats.Size)
}

func TestClient_BuffersOnCircuitBreakerOpen(t *testing.T) {
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.WriteHeader(500)
	}))
	defer srv.Close()

	cb := resilience.GetGlobalCircuitBreaker()
	cb.Reset()
	defer cb.Reset()

	client := newTestClient(t, srv.URL)

	for i := 0; i < 10; i++ {
		payload := NewPayload(OperationChat, "gpt-4", "openai").
			WithTokens(10, 20, 30).
			Build()
		_ = client.SendSync(payload)
	}

	stats := client.GetBufferStats()
	assert.Greater(t, stats.Size, 0)
}

func TestClient_FlushDrainsBuffer(t *testing.T) {
	var callCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c := callCount.Add(1)
		if c <= 4 {
			w.WriteHeader(500)
			return
		}
		w.WriteHeader(200)
	}))
	defer srv.Close()

	client := newTestClient(t, srv.URL)

	payload := NewPayload(OperationChat, "gpt-4", "openai").
		WithTokens(10, 20, 30).
		Build()

	_ = client.SendSync(payload)

	require.Equal(t, 1, client.GetBufferStats().Size)

	client.Flush()

	assert.Equal(t, 0, client.GetBufferStats().Size)
}

func TestClient_ToolEventBufferedAfterFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer srv.Close()

	client := newTestClient(t, srv.URL)

	payload := &ToolEventPayload{
		IdempotencyKey: "tool-key-1",
		ToolID:         "test-tool",
	}

	_ = client.SendToolEventSync(payload)

	stats := client.GetBufferStats()
	assert.Equal(t, 1, stats.Size)
}

func TestClient_CloseStopsBuffer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	defer srv.Close()

	client := newTestClient(t, srv.URL)

	err := client.Close()
	require.NoError(t, err)
}

func TestBuffer_EmptyFlushIsNoop(t *testing.T) {
	buf := newTestBuffer(100)
	buf.flush()
	stats := buf.GetBufferStats()
	assert.Equal(t, 0, stats.Size)
	assert.Equal(t, int64(0), stats.EventsReplayed)
}
