package groq

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

func newTestGroq(t *testing.T, groqURL, meteringURL string) *ReveniumGroq {
	t.Helper()
	r, err := NewReveniumGroq(&Config{
		GroqAPIKey: "gsk-test",
		BaseURL:    groqURL,
		Revenium: &core.ReveniumConfig{
			APIKey:  "hak_test_key_123",
			BaseURL: meteringURL,
		},
	})
	require.NoError(t, err)
	return r
}

func TestChatCompletionsMapsCachedPromptTokensToCacheRead(t *testing.T) {
	groqServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/chat/completions", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, err := w.Write([]byte(`{
			"id": "chatcmpl-groq-cache-hit",
			"object": "chat.completion",
			"created": 1700000000,
			"model": "openai/gpt-oss-120b",
			"choices": [{
				"index": 0,
				"message": {"role": "assistant", "content": "cached response"},
				"finish_reason": "stop"
			}],
			"usage": {
				"prompt_tokens": 4641,
				"completion_tokens": 1817,
				"total_tokens": 6458,
				"prompt_tokens_details": {
					"cached_tokens": 4608
				},
				"completion_tokens_details": {
					"reasoning_tokens": 256
				}
			}
		}`))
		assert.NoError(t, err)
	}))
	defer groqServer.Close()

	mock := testutil.NewMockMeteringServer()
	defer mock.Close()

	r := newTestGroq(t, groqServer.URL, mock.URL())
	defer r.Close()

	resp, err := r.ChatCompletions(context.Background(), ChatCompletionRequest{
		Model:    "openai/gpt-oss-120b",
		Messages: []ChatMessage{{Role: "user", Content: "hello"}},
	})
	require.NoError(t, err)
	require.NotNil(t, resp.Usage.PromptTokensDetails)
	assert.Equal(t, 4608, resp.Usage.PromptTokensDetails.CachedTokens)
	require.NotNil(t, resp.Usage.CompletionTokensDetails)
	assert.Equal(t, 256, resp.Usage.CompletionTokensDetails.ReasoningTokens)

	r.Flush()
	require.True(t, mock.WaitForPayloads(1, 2*time.Second))
	payloads := mock.GetPayloads()
	require.Len(t, payloads, 1)
	payload := payloads[0]

	assert.Equal(t, float64(4641), payload["inputTokenCount"])
	assert.Equal(t, float64(1817), payload["outputTokenCount"])
	assert.Equal(t, float64(6458), payload["totalTokenCount"])
	assert.Equal(t, float64(4608), payload["cacheReadTokenCount"])
	assert.Equal(t, float64(0), payload["cacheCreationTokenCount"])
	assert.Equal(t, float64(256), payload["reasoningTokenCount"])
}

func TestChatCompletionsDefaultsCacheReadTokensToZero(t *testing.T) {
	groqServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/chat/completions", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, err := w.Write([]byte(`{
			"id": "chatcmpl-groq-cache-miss",
			"object": "chat.completion",
			"created": 1700000000,
			"model": "openai/gpt-oss-120b",
			"choices": [{
				"index": 0,
				"message": {"role": "assistant", "content": "uncached response"},
				"finish_reason": "stop"
			}],
			"usage": {
				"prompt_tokens": 4641,
				"completion_tokens": 1817,
				"total_tokens": 6458
			}
		}`))
		assert.NoError(t, err)
	}))
	defer groqServer.Close()

	mock := testutil.NewMockMeteringServer()
	defer mock.Close()

	r := newTestGroq(t, groqServer.URL, mock.URL())
	defer r.Close()

	resp, err := r.ChatCompletions(context.Background(), ChatCompletionRequest{
		Model:    "openai/gpt-oss-120b",
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

	assert.Equal(t, float64(4641), payload["inputTokenCount"])
	assert.Equal(t, float64(1817), payload["outputTokenCount"])
	assert.Equal(t, float64(6458), payload["totalTokenCount"])
	assert.Equal(t, float64(0), payload["cacheReadTokenCount"])
	assert.Equal(t, float64(0), payload["cacheCreationTokenCount"])
	assert.Equal(t, float64(0), payload["reasoningTokenCount"])
}
