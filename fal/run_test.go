package fal

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestFal(t *testing.T, falURL string) *ReveniumFal {
	t.Helper()
	cfg := &Config{
		FalAPIKey:      "test-key",
		FalBaseURL:     falURL,
		QueueBaseURL:   falURL,
		RequestTimeout: 10 * time.Second,
		Revenium:       &core.ReveniumConfig{APIKey: "hak_test_key_123"},
	}
	client, err := NewReveniumFal(cfg)
	require.NoError(t, err)
	return client
}

func TestRun_PostsToEndpointWithAuth(t *testing.T) {
	var capturedAuth, capturedPath string
	var capturedBody map[string]interface{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedAuth = r.Header.Get("Authorization")
		capturedPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&capturedBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"images":[{"url":"https://x/img.png","width":512,"height":512}]}`))
	}))
	defer server.Close()

	client := newTestFal(t, server.URL)
	defer client.Close()

	result, err := client.Run(context.Background(), "fal-ai/flux/schnell", map[string]interface{}{"prompt": "cat"}, nil)
	require.NoError(t, err)

	assert.Equal(t, "Key test-key", capturedAuth)
	assert.Equal(t, "/fal-ai/flux/schnell", capturedPath)
	assert.Equal(t, "cat", capturedBody["prompt"])
	assert.Contains(t, result, "images")
}

func TestRun_GenerateImageDelegatesToRun(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/fal-ai/flux/schnell", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"images":[{"url":"https://x/i.png","width":1024,"height":1024}],"seed":42}`))
	}))
	defer server.Close()

	client := newTestFal(t, server.URL)
	defer client.Close()

	resp, err := client.GenerateImage(context.Background(), "fal-ai/flux/schnell", &FalRequest{Prompt: "ok"})
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Len(t, resp.Images, 1)
	assert.Equal(t, "https://x/i.png", resp.Images[0].URL)
	assert.Equal(t, 42, resp.Seed)
}

func TestRun_DisabledSkipsMetering(t *testing.T) {
	hits := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"images":[]}`))
	}))
	defer server.Close()

	client := newTestFal(t, server.URL)
	defer client.Close()
	client.Disable()
	assert.False(t, client.IsEnabled())

	_, err := client.Run(context.Background(), "fal-ai/flux/dev", nil, nil)
	require.NoError(t, err)
	assert.Equal(t, 1, hits)
}

func TestRun_VideoRequestedDurationFromInput(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"video":{"url":"https://x/v.mp4","duration":5.0}}`))
	}))
	defer server.Close()

	client := newTestFal(t, server.URL)
	defer client.Close()

	result, err := client.Run(context.Background(), "fal-ai/kling-video/v1/standard/text-to-video", map[string]interface{}{
		"prompt":   "boat",
		"duration": "5",
	}, nil)
	require.NoError(t, err)
	v, ok := result["video"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "https://x/v.mp4", v["url"])
}

func TestRun_ProviderErrorOnNon2xx(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"bad input","message":"missing prompt"}`))
	}))
	defer server.Close()

	client := newTestFal(t, server.URL)
	defer client.Close()

	_, err := client.Run(context.Background(), "fal-ai/flux/dev", map[string]interface{}{}, nil)
	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "missing prompt") || strings.Contains(err.Error(), "bad input"))
}

func TestRun_ErrorBodyTruncatedWhenLarge(t *testing.T) {
	hugeBody := strings.Repeat("X", 4096)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(hugeBody))
	}))
	defer server.Close()

	client := newTestFal(t, server.URL)
	defer client.Close()

	_, err := client.Run(context.Background(), "fal-ai/flux/dev", nil, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "truncated")
	assert.Less(t, len(err.Error()), 1024)
}
