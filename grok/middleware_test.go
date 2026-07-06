package grok

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/revenium/revenium-go-sdk/core/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestGrok(t *testing.T, grokURL, meteringURL string) *ReveniumGrok {
	t.Helper()
	r, err := NewReveniumGrok(&Config{
		XAIAPIKey: "xai-test",
		BaseURL:   grokURL,
		Revenium: &core.ReveniumConfig{
			APIKey:  "hak_test_key_123",
			BaseURL: meteringURL,
		},
	})
	require.NoError(t, err)
	return r
}

func TestChatCompletionsMapsCachedPromptTokensToCacheRead(t *testing.T) {
	grokServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/chat/completions", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, err := w.Write([]byte(`{
			"id": "chatcmpl-grok-cache-hit",
			"object": "chat.completion",
			"created": 1700000000,
			"model": "grok-4",
			"choices": [{
				"index": 0,
				"message": {"role": "assistant", "content": "cached response"},
				"finish_reason": "stop"
			}],
			"usage": {
				"prompt_tokens": 1024,
				"completion_tokens": 128,
				"total_tokens": 1152,
				"prompt_tokens_details": {
					"cached_tokens": 768
				},
				"completion_tokens_details": {
					"reasoning_tokens": 32
				}
			}
		}`))
		assert.NoError(t, err)
	}))
	defer grokServer.Close()

	mock := testutil.NewMockMeteringServer()
	defer mock.Close()

	r := newTestGrok(t, grokServer.URL, mock.URL())
	defer r.Close()

	resp, err := r.ChatCompletions(context.Background(), ChatCompletionRequest{
		Model:    "grok-4",
		Messages: []ChatMessage{{Role: "user", Content: "hello"}},
	})
	require.NoError(t, err)
	require.NotNil(t, resp.Usage.PromptTokensDetails)
	assert.Equal(t, 768, resp.Usage.PromptTokensDetails.CachedTokens)
	require.NotNil(t, resp.Usage.CompletionTokensDetails)
	assert.Equal(t, 32, resp.Usage.CompletionTokensDetails.ReasoningTokens)

	r.Flush()
	require.True(t, mock.WaitForPayloads(1, 2*time.Second))
	payloads := mock.GetPayloads()
	require.Len(t, payloads, 1)
	payload := payloads[0]

	assert.Equal(t, float64(1024), payload["inputTokenCount"])
	assert.Equal(t, float64(128), payload["outputTokenCount"])
	assert.Equal(t, float64(1152), payload["totalTokenCount"])
	assert.Equal(t, float64(768), payload["cacheReadTokenCount"])
	assert.Equal(t, float64(0), payload["cacheCreationTokenCount"])
	assert.Equal(t, float64(32), payload["reasoningTokenCount"])
}

func TestChatCompletionsDefaultsCacheReadTokensToZero(t *testing.T) {
	grokServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/chat/completions", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, err := w.Write([]byte(`{
			"id": "chatcmpl-grok-cache-miss",
			"object": "chat.completion",
			"created": 1700000000,
			"model": "grok-4",
			"choices": [{
				"index": 0,
				"message": {"role": "assistant", "content": "uncached response"},
				"finish_reason": "stop"
			}],
			"usage": {
				"prompt_tokens": 1024,
				"completion_tokens": 128,
				"total_tokens": 1152
			}
		}`))
		assert.NoError(t, err)
	}))
	defer grokServer.Close()

	mock := testutil.NewMockMeteringServer()
	defer mock.Close()

	r := newTestGrok(t, grokServer.URL, mock.URL())
	defer r.Close()

	resp, err := r.ChatCompletions(context.Background(), ChatCompletionRequest{
		Model:    "grok-4",
		Messages: []ChatMessage{{Role: "user", Content: "hello"}},
	})
	require.NoError(t, err)
	assert.Nil(t, resp.Usage.PromptTokensDetails)
	assert.Nil(t, resp.Usage.CompletionTokensDetails)

	r.Flush()
	require.True(t, mock.WaitForPayloads(1, 2*time.Second))
	payloads := mock.GetPayloads()
	require.Len(t, payloads, 1)
	payload := payloads[0]

	assert.Equal(t, float64(1024), payload["inputTokenCount"])
	assert.Equal(t, float64(128), payload["outputTokenCount"])
	assert.Equal(t, float64(1152), payload["totalTokenCount"])
	assert.Equal(t, float64(0), payload["cacheReadTokenCount"])
	assert.Equal(t, float64(0), payload["cacheCreationTokenCount"])
	assert.Equal(t, float64(0), payload["reasoningTokenCount"])
}
