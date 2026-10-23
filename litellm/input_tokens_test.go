package litellm

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/revenium/revenium-go-sdk/core/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const documentedCachedUsage = `{"prompt_tokens": 2006, "completion_tokens": 300, "total_tokens": 2306, "prompt_tokens_details": {"cached_tokens": 1920}, "cache_creation_input_tokens": 0}`

const haikuMostlyCachedUsage = `{"prompt_tokens": 10010, "completion_tokens": 5, "total_tokens": 10015, "prompt_tokens_details": {"cached_tokens": 10000}, "cache_creation_input_tokens": 0}`

const haikuCacheWriteUsage = `{"prompt_tokens": 10010, "completion_tokens": 5, "total_tokens": 10015, "prompt_tokens_details": {"cached_tokens": 0, "cache_creation_tokens": 10000}, "cache_creation_input_tokens": 10000}`

const openAICachedUsage = `{"prompt_tokens": 1000, "completion_tokens": 50, "total_tokens": 1050, "prompt_tokens_details": {"cached_tokens": 800}}`

type meteredCounts struct {
	input, cacheRead, cacheCreation, output, total float64
}

func meterCompletion(t *testing.T, model, usage string) map[string]interface{} {
	t.Helper()
	return meterAliasedCompletion(t, model, model, usage)
}

func meterAliasedCompletion(t *testing.T, requestedModel, servedModel, usage string) map[string]interface{} {
	t.Helper()
	proxy := newJSONProxy(t, "/chat/completions", fmt.Sprintf(`{
		"id": "chatcmpl-litellm-cache",
		"object": "chat.completion",
		"created": 1700000000,
		"model": %q,
		"choices": [{"index": 0, "message": {"role": "assistant", "content": "ok"}, "finish_reason": "stop"}],
		"usage": %s
	}`, servedModel, usage))
	defer proxy.Close()

	mock := testutil.NewMockMeteringServer()
	defer mock.Close()

	r := newTestLiteLLM(t, proxy.URL, mock.URL())
	defer r.Close()

	_, err := r.Chat().Completions().New(context.Background(), ChatCompletionRequest{
		Model:    requestedModel,
		Messages: []ChatMessage{{Role: "user", Content: "hello"}},
	})
	require.NoError(t, err)
	return singlePayload(t, r, mock)
}

func meterStream(t *testing.T, model, usage string) map[string]interface{} {
	t.Helper()
	return meterAliasedStream(t, model, model, usage)
}

func meterAliasedStream(t *testing.T, requestedModel, servedModel, usage string) map[string]interface{} {
	t.Helper()
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, err := fmt.Fprintf(w,
			"data: {\"id\":\"resp-stream-cache\",\"model\":%q,\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\"}}]}\n\n"+
				"data: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":%s}\n\n"+
				"data: [DONE]\n\n", servedModel, usage)
		assert.NoError(t, err)
	}))
	defer proxy.Close()

	mock := testutil.NewMockMeteringServer()
	defer mock.Close()

	r := newTestLiteLLM(t, proxy.URL, mock.URL())
	defer r.Close()

	stream, err := r.Chat().Completions().NewStreaming(context.Background(), ChatCompletionRequest{
		Model:    requestedModel,
		Messages: []ChatMessage{{Role: "user", Content: "hello"}},
	})
	require.NoError(t, err)
	for stream.Next() {
	}
	require.NoError(t, stream.Err())
	require.NoError(t, stream.Close())
	return singlePayload(t, r, mock)
}

func assertCounts(t *testing.T, want meteredCounts, payload map[string]interface{}) {
	t.Helper()
	assert.Equal(t, want.input, payload["inputTokenCount"], "inputTokenCount")
	assert.Equal(t, want.cacheRead, payload["cacheReadTokenCount"], "cacheReadTokenCount")
	assert.Equal(t, want.cacheCreation, payload["cacheCreationTokenCount"], "cacheCreationTokenCount")
	assert.Equal(t, want.output, payload["outputTokenCount"], "outputTokenCount")
	assert.Equal(t, want.total, payload["totalTokenCount"], "totalTokenCount")
}

func TestClaudeCachedTokensAreCountedOnce(t *testing.T) {
	tests := []struct {
		name  string
		model string
		usage string
		want  meteredCounts
	}{
		{
			name:  "LiteLLM documented cache read shape",
			model: "anthropic/claude-sonnet-4-5-20250929",
			usage: documentedCachedUsage,
			want:  meteredCounts{input: 86, cacheRead: 1920, output: 300, total: 2306},
		},
		{
			name:  "mostly cached haiku prompt matches Anthropic's own input_tokens",
			model: "anthropic/claude-haiku-4-5-20251001",
			usage: haikuMostlyCachedUsage,
			want:  meteredCounts{input: 10, cacheRead: 10000, output: 5, total: 10015},
		},
		{
			name:  "cache writes are taken out too",
			model: "claude-haiku-4-5-20251001",
			usage: haikuCacheWriteUsage,
			want:  meteredCounts{input: 10, cacheCreation: 10000, output: 5, total: 10015},
		},
		{
			name:  "Claude on Bedrock",
			model: "bedrock/anthropic.claude-3-5-haiku-20241022-v1:0",
			usage: haikuMostlyCachedUsage,
			want:  meteredCounts{input: 10, cacheRead: 10000, output: 5, total: 10015},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name+"/non-streaming", func(t *testing.T) {
			assertCounts(t, tt.want, meterCompletion(t, tt.model, tt.usage))
		})
		t.Run(tt.name+"/streaming", func(t *testing.T) {
			assertCounts(t, tt.want, meterStream(t, tt.model, tt.usage))
		})
	}
}

func TestAliasRoutedToClaudeIsNettedByTheServedModel(t *testing.T) {
	want := meteredCounts{input: 10, cacheRead: 10000, output: 5, total: 10015}

	t.Run("streaming", func(t *testing.T) {
		payload := meterAliasedStream(t, "support-model", "claude-haiku-4-5-20251001", haikuMostlyCachedUsage)
		assertCounts(t, want, payload)
		assert.Equal(t, "claude-haiku-4-5-20251001", payload["model"])
		assert.Equal(t, "Anthropic", payload["provider"])
	})
	t.Run("non-streaming", func(t *testing.T) {
		payload := meterAliasedCompletion(t, "support-model", "claude-haiku-4-5-20251001", haikuMostlyCachedUsage)
		assertCounts(t, want, payload)
		assert.Equal(t, "claude-haiku-4-5-20251001", payload["model"])
		assert.Equal(t, "Anthropic", payload["provider"])
	})
}

func TestStreamWithoutModelInChunksFallsBackToRequestedModel(t *testing.T) {
	payload := meterAliasedStream(t, "anthropic/claude-haiku-4-5-20251001", "", haikuMostlyCachedUsage)

	assertCounts(t, meteredCounts{input: 10, cacheRead: 10000, output: 5, total: 10015}, payload)
	assert.Equal(t, "claude-haiku-4-5-20251001", payload["model"])
}

func TestOpenAIShapedPromptStaysGrossForPlatformToNet(t *testing.T) {
	want := meteredCounts{input: 1000, cacheRead: 800, output: 50, total: 1050}

	assertCounts(t, want, meterCompletion(t, "openai/gpt-4o-mini", openAICachedUsage))
	assertCounts(t, want, meterStream(t, "openai/gpt-4o-mini", openAICachedUsage))
}

func TestBillableInputTokens(t *testing.T) {
	tests := []struct {
		name                                    string
		model                                   string
		prompt, cacheRead, cacheCreation, wants int64
	}{
		{name: "claude nets both cache buckets", model: "claude-sonnet-4-5", prompt: 5120, cacheRead: 768, cacheCreation: 4096, wants: 256},
		{name: "claude without cache is unchanged", model: "anthropic/claude-sonnet-4-5", prompt: 898, wants: 898},
		{name: "claude prompt smaller than cache is left as reported", model: "claude-sonnet-4-5", prompt: 100, cacheRead: 768, wants: 100},
		{name: "claude cache-only usage chunk stays zero", model: "claude-sonnet-4-5", cacheRead: 768, cacheCreation: 4096, wants: 0},
		{name: "openai stays gross", model: "openai/gpt-4o", prompt: 1000, cacheRead: 800, wants: 1000},
		{name: "gemini stays gross", model: "gemini/gemini-2.5-flash", prompt: 1000, cacheRead: 800, wants: 1000},
		{name: "xai stays gross", model: "xai/grok-4", prompt: 1000, cacheRead: 800, wants: 1000},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.wants, billableInputTokens(tt.model, tt.prompt, tt.cacheRead, tt.cacheCreation))
		})
	}
}
