package litellm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/revenium/revenium-go-sdk/core/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func claudeResponseWithUsage(usage string) string {
	return `{
		"id": "chatcmpl-litellm-ttl",
		"object": "chat.completion",
		"created": 1700000000,
		"model": "anthropic/claude-sonnet-4-20250514",
		"choices": [{"index": 0, "message": {"role": "assistant", "content": "ok"}, "finish_reason": "stop"}],
		"usage": ` + usage + `
	}`
}

func meterOneClaudeCall(t *testing.T, usage string) map[string]interface{} {
	t.Helper()
	proxy := newJSONProxy(t, "/chat/completions", claudeResponseWithUsage(usage))
	defer proxy.Close()
	mock := testutil.NewMockMeteringServer()
	defer mock.Close()
	r := newTestLiteLLM(t, proxy.URL, mock.URL())
	defer r.Close()

	_, err := r.Chat().Completions().New(context.Background(), ChatCompletionRequest{
		Model:    "anthropic/claude-sonnet-4-20250514",
		Messages: []ChatMessage{{Role: "user", Content: "hello"}},
	})
	require.NoError(t, err)
	return singlePayload(t, r, mock)
}

func TestCompletionsSendsClaudeCacheWriteSplitByTTL(t *testing.T) {
	payload := meterOneClaudeCall(t, `{
		"prompt_tokens": 5120, "completion_tokens": 128, "total_tokens": 5248,
		"cache_creation_input_tokens": 4096,
		"prompt_tokens_details": {
			"cached_tokens": 768,
			"cache_creation_tokens": 4096,
			"cache_creation_token_details": {"ephemeral_5m_input_tokens": 1024, "ephemeral_1h_input_tokens": 3072}
		}
	}`)

	assert.Equal(t, float64(4096), payload["cacheCreationTokenCount"])
	assert.Equal(t, float64(1024), payload["cacheCreation5mTokenCount"])
	assert.Equal(t, float64(3072), payload["cacheCreation1hTokenCount"])
}

func TestCompletionsLeavesOutTheTTLBucketLiteLLMDidNotReport(t *testing.T) {
	payload := meterOneClaudeCall(t, `{
		"prompt_tokens": 5120, "completion_tokens": 128, "total_tokens": 5248,
		"prompt_tokens_details": {
			"cache_creation_tokens": 4096,
			"cache_creation_token_details": {"ephemeral_1h_input_tokens": 4096}
		}
	}`)

	assert.Equal(t, float64(4096), payload["cacheCreation1hTokenCount"])
	assert.NotContains(t, payload, "cacheCreation5mTokenCount")
}

func TestCompletionsSendsAReportedZeroBucketAsZero(t *testing.T) {
	payload := meterOneClaudeCall(t, `{
		"prompt_tokens": 5120, "completion_tokens": 128, "total_tokens": 5248,
		"prompt_tokens_details": {
			"cache_creation_tokens": 4096,
			"cache_creation_token_details": {"ephemeral_5m_input_tokens": 0, "ephemeral_1h_input_tokens": 4096}
		}
	}`)

	assert.Equal(t, float64(0), payload["cacheCreation5mTokenCount"])
	assert.Equal(t, float64(4096), payload["cacheCreation1hTokenCount"])
}

func TestCompletionsWithoutTheSplitSendsOnlyTheTotal(t *testing.T) {
	payload := meterOneClaudeCall(t, `{
		"prompt_tokens": 5120, "completion_tokens": 128, "total_tokens": 5248,
		"prompt_tokens_details": {"cache_creation_tokens": 4096}
	}`)

	assert.Equal(t, float64(4096), payload["cacheCreationTokenCount"])
	assert.NotContains(t, payload, "cacheCreation5mTokenCount")
	assert.NotContains(t, payload, "cacheCreation1hTokenCount")
}

func TestStreamingSendsClaudeCacheWriteSplitByTTL(t *testing.T) {
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, err := w.Write([]byte(
			"data: {\"id\":\"resp-stream-ttl\",\"model\":\"anthropic/claude-sonnet-4-20250514\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hi\"}}]}\n\n" +
				"data: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":5120,\"completion_tokens\":128,\"total_tokens\":5248," +
				"\"prompt_tokens_details\":{\"cache_creation_tokens\":4096,\"cache_creation_token_details\":{\"ephemeral_5m_input_tokens\":96,\"ephemeral_1h_input_tokens\":4000}}}}\n\n" +
				"data: {\"choices\":[],\"usage\":{\"prompt_tokens\":5120,\"completion_tokens\":128,\"total_tokens\":5248}}\n\n" +
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
	assert.Equal(t, float64(96), payload["cacheCreation5mTokenCount"])
	assert.Equal(t, float64(4000), payload["cacheCreation1hTokenCount"])
}
