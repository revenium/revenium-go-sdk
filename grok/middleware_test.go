package grok

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/revenium/revenium-go-sdk/core/metering"
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

const capturedXAIReasoningResponse = `{
	"id": "vbb-1791442624-grok",
	"object": "chat.completion",
	"created": 1791442624,
	"model": "grok-4.3",
	"choices": [{
		"index": 0,
		"message": {"role": "assistant", "content": "391"},
		"finish_reason": "stop"
	}],
	"usage": {
		"prompt_tokens": 206,
		"completion_tokens": 1,
		"total_tokens": 424,
		"prompt_tokens_details": {"text_tokens": 206, "audio_tokens": 0, "image_tokens": 0, "cached_tokens": 192},
		"completion_tokens_details": {"reasoning_tokens": 217, "audio_tokens": 0, "accepted_prediction_tokens": 0, "rejected_prediction_tokens": 0},
		"num_sources_used": 0,
		"cost_in_usd_ticks": 6009000
	}
}`

func meterOneGrokCall(t *testing.T, responseBody, requestedModel string) map[string]interface{} {
	t.Helper()
	grokServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, err := w.Write([]byte(responseBody))
		assert.NoError(t, err)
	}))
	defer grokServer.Close()

	mock := testutil.NewMockMeteringServer()
	defer mock.Close()

	r := newTestGrok(t, grokServer.URL, mock.URL())
	defer r.Close()

	_, err := r.ChatCompletions(context.Background(), ChatCompletionRequest{
		Model:    requestedModel,
		Messages: []ChatMessage{{Role: "user", Content: "What is 17 times 23? Answer with the number only."}},
	})
	require.NoError(t, err)

	r.Flush()
	require.True(t, mock.WaitForPayloads(1, 2*time.Second))
	payloads := mock.GetPayloads()
	require.Len(t, payloads, 1)
	return payloads[0]
}

func TestChatCompletionsForwardsXAIReportedCost(t *testing.T) {
	payload := meterOneGrokCall(t, capturedXAIReasoningResponse, "grok-3-mini")

	require.Contains(t, payload, "totalCost")
	assert.InDelta(t, 0.0006009, payload["totalCost"], 1e-12)
	assert.NotContains(t, payload, "inputTokenCost")
	assert.NotContains(t, payload, "outputTokenCost")
}

func TestChatCompletionsBillsReasoningTokensAsOutput(t *testing.T) {
	payload := meterOneGrokCall(t, capturedXAIReasoningResponse, "grok-3-mini")

	assert.Equal(t, float64(206), payload["inputTokenCount"])
	assert.Equal(t, float64(218), payload["outputTokenCount"])
	assert.Equal(t, float64(217), payload["reasoningTokenCount"])
	assert.Equal(t, float64(192), payload["cacheReadTokenCount"])
	assert.Equal(t, float64(424), payload["totalTokenCount"])
}

func TestChatCompletionsMetersTheModelXAIServed(t *testing.T) {
	payload := meterOneGrokCall(t, capturedXAIReasoningResponse, "grok-3-mini")

	assert.Equal(t, "grok-4.3", payload["model"])
	assert.Equal(t, "XAI", payload["provider"])
}

func TestChatCompletionsFallsBackToRequestedModelWhenXAIOmitsIt(t *testing.T) {
	payload := meterOneGrokCall(t, `{
		"id": "chatcmpl-grok-no-model",
		"choices": [{"index": 0, "message": {"role": "assistant", "content": "hi"}, "finish_reason": "stop"}],
		"usage": {"prompt_tokens": 10, "completion_tokens": 2, "total_tokens": 12}
	}`, "grok-3-mini")

	assert.Equal(t, "grok-3-mini", payload["model"])
}

func TestChatCompletionsLeavesCostToRateCardWhenXAIReportsNone(t *testing.T) {
	payload := meterOneGrokCall(t, `{
		"id": "chatcmpl-grok-no-cost",
		"model": "grok-4",
		"choices": [{"index": 0, "message": {"role": "assistant", "content": "hi"}, "finish_reason": "stop"}],
		"usage": {"prompt_tokens": 10, "completion_tokens": 2, "total_tokens": 12}
	}`, "grok-4")

	assert.NotContains(t, payload, "totalCost")
	assert.Equal(t, float64(2), payload["outputTokenCount"])
}

func TestBilledOutputTokens(t *testing.T) {
	tests := []struct {
		name     string
		usage    Usage
		expected int64
	}{
		{
			name:     "reasoning reported outside completion is added",
			usage:    Usage{PromptTokens: 198, CompletionTokens: 3, TotalTokens: 388, CompletionTokensDetails: reasoningDetails(187)},
			expected: 190,
		},
		{
			name:     "reasoning already inside completion is not added again",
			usage:    Usage{PromptTokens: 1024, CompletionTokens: 128, TotalTokens: 1152, CompletionTokensDetails: reasoningDetails(32)},
			expected: 128,
		},
		{
			name:     "missing total still bills reported reasoning",
			usage:    Usage{PromptTokens: 10, CompletionTokens: 2, CompletionTokensDetails: reasoningDetails(40)},
			expected: 42,
		},
		{
			name:     "no reasoning details",
			usage:    Usage{PromptTokens: 10, CompletionTokens: 2, TotalTokens: 12},
			expected: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.usage.billedOutputTokens())
		})
	}
}

func TestReportedCostUSD(t *testing.T) {
	ticks := func(v int64) *int64 { return &v }

	assert.Nil(t, Usage{}.reportedCostUSD())
	assert.Nil(t, Usage{CostInUSDTicks: ticks(-1)}.reportedCostUSD())

	zero := Usage{CostInUSDTicks: ticks(0)}.reportedCostUSD()
	require.NotNil(t, zero)
	assert.Equal(t, 0.0, *zero)

	cost := Usage{CostInUSDTicks: ticks(5881000)}.reportedCostUSD()
	require.NotNil(t, cost)
	assert.InDelta(t, 0.0005881, *cost, 1e-12)
}

func reasoningDetails(tokens int) *metering.CompletionTokensDetails {
	return &metering.CompletionTokensDetails{ReasoningTokens: tokens}
}
