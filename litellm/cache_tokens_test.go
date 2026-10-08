package litellm

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/revenium/revenium-go-sdk/core/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestLiteLLM(t *testing.T, proxyURL, meteringURL string) *ReveniumLiteLLM {
	t.Helper()
	r, err := NewReveniumLiteLLM(&Config{
		LiteLLMProxyURL: proxyURL,
		LiteLLMAPIKey:   "sk-litellm-test",
		Revenium: &core.ReveniumConfig{
			APIKey:  "hak_test_key_123",
			BaseURL: meteringURL,
		},
	})
	require.NoError(t, err)
	return r
}

func newJSONProxy(t *testing.T, expectedPath, body string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, expectedPath, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, err := w.Write([]byte(body))
		assert.NoError(t, err)
	}))
}

func singlePayload(t *testing.T, r *ReveniumLiteLLM, mock *testutil.MockMeteringServer) map[string]interface{} {
	t.Helper()
	r.Flush()
	require.True(t, mock.WaitForPayloads(1, 2*time.Second))
	payloads := mock.GetPayloads()
	require.Len(t, payloads, 1)
	return payloads[0]
}

func TestCompletionsMapsCacheCreationTokensToPayload(t *testing.T) {
	proxy := newJSONProxy(t, "/chat/completions", `{
		"id": "chatcmpl-litellm-cache-write",
		"object": "chat.completion",
		"created": 1700000000,
		"model": "anthropic/claude-sonnet-4-20250514",
		"choices": [{
			"index": 0,
			"message": {"role": "assistant", "content": "cached response"},
			"finish_reason": "stop"
		}],
		"usage": {
			"prompt_tokens": 5120,
			"completion_tokens": 128,
			"total_tokens": 5248,
			"prompt_tokens_details": {
				"cached_tokens": 768,
				"cache_creation_tokens": 4096
			}
		}
	}`)
	defer proxy.Close()

	mock := testutil.NewMockMeteringServer()
	defer mock.Close()

	r := newTestLiteLLM(t, proxy.URL, mock.URL())
	defer r.Close()

	resp, err := r.Chat().Completions().New(context.Background(), ChatCompletionRequest{
		Model:    "anthropic/claude-sonnet-4-20250514",
		Messages: []ChatMessage{{Role: "user", Content: "hello"}},
	})
	require.NoError(t, err)
	require.NotNil(t, resp.Usage)

	payload := singlePayload(t, r, mock)

	assert.Equal(t, float64(4096), payload["cacheCreationTokenCount"])
	assert.Equal(t, float64(768), payload["cacheReadTokenCount"])
	assert.Equal(t, float64(5120), payload["inputTokenCount"])
	assert.Equal(t, float64(128), payload["outputTokenCount"])
	assert.Equal(t, float64(5248), payload["totalTokenCount"])
}

func TestStreamingMapsCacheCreationTokensToPayload(t *testing.T) {
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/chat/completions", r.URL.Path)
		w.Header().Set("Content-Type", "text/event-stream")
		_, err := w.Write([]byte(
			"data: {\"id\":\"resp-stream-cache\",\"model\":\"anthropic/claude-sonnet-4-20250514\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hi\"}}]}\n\n" +
				"data: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":5120,\"completion_tokens\":128,\"total_tokens\":5248,\"cache_creation_input_tokens\":4096}}\n\n" +
				"data: [DONE]\n\n"))
		assert.NoError(t, err)
	}))
	defer proxy.Close()

	mock := testutil.NewMockMeteringServer()
	defer mock.Close()

	r := newTestLiteLLM(t, proxy.URL, mock.URL())
	defer r.Close()

	stream, err := r.Chat().Completions().NewStreaming(context.Background(), ChatCompletionRequest{
		Model:    "anthropic/claude-sonnet-4-20250514",
		Messages: []ChatMessage{{Role: "user", Content: "hello"}},
	})
	require.NoError(t, err)
	for stream.Next() {
	}
	require.NoError(t, stream.Err())
	require.NoError(t, stream.Close())

	payload := singlePayload(t, r, mock)

	assert.Equal(t, float64(4096), payload["cacheCreationTokenCount"])
	assert.Equal(t, float64(5120), payload["inputTokenCount"])
	assert.Equal(t, float64(128), payload["outputTokenCount"])
	assert.Equal(t, float64(5248), payload["totalTokenCount"])
}

func TestStreamingCapturesCacheOnlyUsageChunk(t *testing.T) {
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/chat/completions", r.URL.Path)
		w.Header().Set("Content-Type", "text/event-stream")
		_, err := w.Write([]byte(
			"data: {\"id\":\"resp-stream-cache-only\",\"model\":\"anthropic/claude-sonnet-4-20250514\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hi\"}}]}\n\n" +
				"data: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":0,\"completion_tokens\":0,\"total_tokens\":0,\"prompt_tokens_details\":{\"cached_tokens\":768,\"cache_creation_tokens\":4096}}}\n\n" +
				"data: [DONE]\n\n"))
		assert.NoError(t, err)
	}))
	defer proxy.Close()

	mock := testutil.NewMockMeteringServer()
	defer mock.Close()

	r := newTestLiteLLM(t, proxy.URL, mock.URL())
	defer r.Close()

	stream, err := r.Chat().Completions().NewStreaming(context.Background(), ChatCompletionRequest{
		Model:    "anthropic/claude-sonnet-4-20250514",
		Messages: []ChatMessage{{Role: "user", Content: "hello"}},
	})
	require.NoError(t, err)
	for stream.Next() {
	}
	require.NoError(t, stream.Err())
	require.NoError(t, stream.Close())

	payload := singlePayload(t, r, mock)

	assert.Equal(t, float64(4096), payload["cacheCreationTokenCount"])
	assert.Equal(t, float64(768), payload["cacheReadTokenCount"])
}

func TestCacheCreationTokenCountPrecedence(t *testing.T) {
	tests := []struct {
		name     string
		usage    string
		expected int64
	}{
		{
			name:     "nested cache_creation_tokens",
			usage:    `{"prompt_tokens_details":{"cache_creation_tokens":4096}}`,
			expected: 4096,
		},
		{
			name:     "flat cache_creation_input_tokens",
			usage:    `{"cache_creation_input_tokens":2048}`,
			expected: 2048,
		},
		{
			name:     "nested cache_write_tokens",
			usage:    `{"prompt_tokens_details":{"cache_write_tokens":1024}}`,
			expected: 1024,
		},
		{
			name:     "nested wins over flat",
			usage:    `{"cache_creation_input_tokens":2048,"prompt_tokens_details":{"cache_creation_tokens":4096}}`,
			expected: 4096,
		},
		{
			name:     "flat wins over cache_write_tokens",
			usage:    `{"cache_creation_input_tokens":2048,"prompt_tokens_details":{"cache_write_tokens":1024}}`,
			expected: 2048,
		},
		{
			name:     "all names absent",
			usage:    `{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}`,
			expected: 0,
		},
		{
			name:     "nil prompt_tokens_details",
			usage:    `{"prompt_tokens":10}`,
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var usage TokenUsage
			require.NoError(t, json.Unmarshal([]byte(tt.usage), &usage))
			assert.Equal(t, tt.expected, usage.CacheCreationTokenCount())
		})
	}

	var nilUsage *TokenUsage
	assert.Equal(t, int64(0), nilUsage.CacheCreationTokenCount())
}

type errorReader struct{}

func (errorReader) Read([]byte) (int, error) { return 0, errors.New("stream transport failure") }

func (errorReader) Close() error { return nil }

func TestCorrectSitesStillEmitZeroCacheCreation(t *testing.T) {
	t.Run("completion error path", func(t *testing.T) {
		proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			_, err := io.WriteString(w, `{"error":"upstream exploded"}`)
			assert.NoError(t, err)
		}))
		defer proxy.Close()

		mock := testutil.NewMockMeteringServer()
		defer mock.Close()

		r := newTestLiteLLM(t, proxy.URL, mock.URL())
		defer r.Close()

		_, err := r.Chat().Completions().New(context.Background(), ChatCompletionRequest{
			Model:    "anthropic/claude-sonnet-4-20250514",
			Messages: []ChatMessage{{Role: "user", Content: "hello"}},
		})
		require.Error(t, err)

		payload := singlePayload(t, r, mock)

		assert.Equal(t, float64(0), payload["cacheCreationTokenCount"])
		assert.Equal(t, float64(0), payload["cacheReadTokenCount"])
		assert.Equal(t, float64(0), payload["inputTokenCount"])
		assert.Equal(t, float64(0), payload["outputTokenCount"])
		assert.Equal(t, float64(0), payload["totalTokenCount"])
	})

	t.Run("streaming error path", func(t *testing.T) {
		mock := testutil.NewMockMeteringServer()
		defer mock.Close()

		r := newTestLiteLLM(t, "http://unused.invalid", mock.URL())
		defer r.Close()

		stream := newStreamingResponse(errorReader{}, nil, "anthropic/claude-sonnet-4-20250514", r.metering, true)
		for stream.Next() {
		}
		require.Error(t, stream.Err())
		require.NoError(t, stream.Close())

		payload := singlePayload(t, r, mock)

		assert.Equal(t, float64(0), payload["cacheCreationTokenCount"])
		assert.Equal(t, float64(0), payload["cacheReadTokenCount"])
		assert.Equal(t, float64(0), payload["inputTokenCount"])
		assert.Equal(t, float64(0), payload["outputTokenCount"])
		assert.Equal(t, float64(0), payload["totalTokenCount"])
	})

	t.Run("embeddings path", func(t *testing.T) {
		proxy := newJSONProxy(t, "/embeddings", `{
			"object": "list",
			"data": [{"object": "embedding", "embedding": [0.1, 0.2], "index": 0}],
			"model": "openai/text-embedding-3-small",
			"usage": {"prompt_tokens": 12, "total_tokens": 12}
		}`)
		defer proxy.Close()

		mock := testutil.NewMockMeteringServer()
		defer mock.Close()

		r := newTestLiteLLM(t, proxy.URL, mock.URL())
		defer r.Close()

		_, err := r.Embeddings().New(context.Background(), EmbeddingRequest{
			Model: "openai/text-embedding-3-small",
			Input: "hello",
		})
		require.NoError(t, err)

		payload := singlePayload(t, r, mock)

		assert.Equal(t, float64(0), payload["cacheCreationTokenCount"])
		assert.Equal(t, float64(0), payload["cacheReadTokenCount"])
		assert.Equal(t, float64(12), payload["inputTokenCount"])
		assert.Equal(t, float64(0), payload["outputTokenCount"])
		assert.Equal(t, float64(12), payload["totalTokenCount"])
	})
}
