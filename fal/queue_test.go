package fal

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSubscribe_QueueLifecycle(t *testing.T) {
	var statusCalls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/fal-ai/kling-video/v1/standard/text-to-video":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"request_id":"req-123"}`))
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/requests/req-123/status"):
			n := atomic.AddInt32(&statusCalls, 1)
			w.Header().Set("Content-Type", "application/json")
			if n < 2 {
				_, _ = w.Write([]byte(`{"status":"IN_PROGRESS","request_id":"req-123"}`))
			} else {
				_, _ = w.Write([]byte(`{"status":"COMPLETED","request_id":"req-123"}`))
			}
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/requests/req-123"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"video":{"url":"https://x/v.mp4","duration":5}}`))
		default:
			t.Logf("unexpected %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	client := newTestFal(t, server.URL)
	defer client.Close()

	result, err := client.Subscribe(context.Background(), "fal-ai/kling-video/v1/standard/text-to-video", map[string]interface{}{"prompt": "boat"}, nil)
	require.NoError(t, err)
	video, ok := result["video"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "https://x/v.mp4", video["url"])
	assert.GreaterOrEqual(t, atomic.LoadInt32(&statusCalls), int32(2))
}

func TestSubscribe_FailedStatusReturnsError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost:
			_, _ = w.Write([]byte(`{"request_id":"r1"}`))
		case strings.HasSuffix(r.URL.Path, "/status"):
			_, _ = w.Write([]byte(`{"status":"FAILED","request_id":"r1"}`))
		}
	}))
	defer server.Close()

	client := newTestFal(t, server.URL)
	defer client.Close()

	_, err := client.Subscribe(context.Background(), "fal-ai/anything", map[string]interface{}{}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "FAILED")
}
