package fal

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStream_PartialEventsThenDone(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/fal-ai/openrouter/llama-3/stream", r.URL.Path)
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, ok := w.(http.Flusher)
		require.True(t, ok)

		_, _ = w.Write([]byte("data: {\"output\":\"hi\",\"partial\":true}\n\n"))
		flusher.Flush()
		_, _ = w.Write([]byte("data: {\"output\":\"hi there\",\"usage\":{\"total_tokens\":3}}\n\n"))
		flusher.Flush()
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
		flusher.Flush()
	}))
	defer server.Close()

	client := newTestFal(t, server.URL)
	defer client.Close()

	events, err := client.Stream(context.Background(), "fal-ai/openrouter/llama-3", map[string]interface{}{"prompt": "say hi"}, nil)
	require.NoError(t, err)

	collected := []StreamEvent{}
	for ev := range events {
		collected = append(collected, ev)
	}
	require.GreaterOrEqual(t, len(collected), 2)

	last := collected[len(collected)-1]
	assert.True(t, last.Done)
	require.NotNil(t, last.Data)
	assert.Equal(t, "hi there", last.Data["output"])
}

func TestStream_ContextCancelStopsGoroutineQuickly(t *testing.T) {
	blockUntilCancel := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		_, _ = w.Write([]byte("data: {\"output\":\"start\"}\n\n"))
		if flusher != nil {
			flusher.Flush()
		}
		<-blockUntilCancel
	}))
	defer server.Close()
	defer close(blockUntilCancel)

	client := newTestFal(t, server.URL)
	defer client.Close()

	ctx, cancel := context.WithCancel(context.Background())
	events, err := client.Stream(ctx, "fal-ai/openrouter/llama-3", nil, nil)
	require.NoError(t, err)

	first, ok := <-events
	require.True(t, ok)
	assert.Equal(t, "start", first.Data["output"])

	cancel()

	done := make(chan struct{})
	go func() {
		for range events {
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("stream goroutine did not exit within 2s after context cancel")
	}
}

func TestStream_NoLeakWhenConsumerStopsReading(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		for i := 0; i < 200; i++ {
			_, _ = w.Write([]byte("data: {\"chunk\":" + string(rune('0'+(i%10))) + "}\n\n"))
			if flusher != nil {
				flusher.Flush()
			}
		}
	}))
	defer server.Close()

	client := newTestFal(t, server.URL)
	defer client.Close()

	ctx, cancel := context.WithCancel(context.Background())
	events, err := client.Stream(ctx, "fal-ai/openrouter/llama-3", nil, nil)
	require.NoError(t, err)

	<-events

	cancel()

	done := make(chan struct{})
	go func() {
		for range events {
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("stream goroutine leaked when consumer stopped reading after ctx cancel")
	}
}

func TestStream_ChannelClosedOnError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("boom"))
	}))
	defer server.Close()

	client := newTestFal(t, server.URL)
	defer client.Close()

	_, err := client.Stream(context.Background(), "fal-ai/openrouter/x", nil, nil)
	require.Error(t, err)
}
