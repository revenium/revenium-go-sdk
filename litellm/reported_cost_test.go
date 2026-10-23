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

const grokUsageWithCost = `{"prompt_tokens":206,"completion_tokens":218,"total_tokens":424,"completion_tokens_details":{"reasoning_tokens":217},"cost":0.0006009}`

func grokCompletionProxy(t *testing.T, usage string) *httptest.Server {
	t.Helper()
	server := newJSONProxy(t, "/chat/completions", `{"id":"chatcmpl-x","object":"chat.completion","created":1700000000,"model":"xai/grok-4.3",`+
		`"choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}],"usage":`+usage+`}`)
	t.Cleanup(server.Close)
	return server
}

func grokStreamProxy(t *testing.T, usage string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for _, line := range []string{
			`data: {"id":"chatcmpl-x","object":"chat.completion.chunk","created":1700000000,"model":"xai/grok-4.3","choices":[{"index":0,"delta":{"role":"assistant","content":"hi"},"finish_reason":null}]}`,
			`data: {"id":"chatcmpl-x","object":"chat.completion.chunk","created":1700000000,"model":"xai/grok-4.3","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":` + usage + `}`,
			"data: [DONE]",
		} {
			_, _ = w.Write([]byte(line + "\n\n"))
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func recordedGrokCall(t *testing.T, usage string, streamed bool) map[string]interface{} {
	t.Helper()
	mock := testutil.NewMockMeteringServer()
	t.Cleanup(mock.Close)
	if !streamed {
		r := newTestLiteLLM(t, grokCompletionProxy(t, usage).URL, mock.URL())
		t.Cleanup(func() { _ = r.Close() })
		_, err := r.Chat().Completions().New(context.Background(), chatRequest("xai/grok-4.3"))
		require.NoError(t, err)
		return singlePayload(t, r, mock)
	}
	r := newTestLiteLLM(t, grokStreamProxy(t, usage).URL, mock.URL())
	t.Cleanup(func() { _ = r.Close() })
	stream, err := r.Chat().Completions().NewStreaming(context.Background(), chatRequest("xai/grok-4.3"))
	require.NoError(t, err)
	for stream.Next() {
	}
	require.NoError(t, stream.Close())
	return singlePayload(t, r, mock)
}

func TestChatCompletions_RecordsTheCostTheProxyReports(t *testing.T) {
	for name, streamed := range map[string]bool{"non-streaming": false, "streaming": true} {
		t.Run(name, func(t *testing.T) {
			payload := recordedGrokCall(t, grokUsageWithCost, streamed)

			assert.InDelta(t, 0.0006009, payload["totalCost"], 1e-12)
			assert.NotContains(t, payload, "inputTokenCost")
			assert.NotContains(t, payload, "outputTokenCost")
			assert.Equal(t, "XAI", payload["provider"])
		})
	}
}

func TestChatCompletions_WithoutAReportedCostSendsNoCost(t *testing.T) {
	usages := map[string]string{
		"no cost field": `{"prompt_tokens":3,"completion_tokens":1,"total_tokens":4}`,
		"null cost":     `{"prompt_tokens":3,"completion_tokens":1,"total_tokens":4,"cost":null}`,
		"negative cost": `{"prompt_tokens":3,"completion_tokens":1,"total_tokens":4,"cost":-1}`,
	}
	for usageName, usage := range usages {
		for name, streamed := range map[string]bool{"non-streaming": false, "streaming": true} {
			t.Run(usageName+"/"+name, func(t *testing.T) {
				payload := recordedGrokCall(t, usage, streamed)

				assert.NotContains(t, payload, "totalCost")
				assert.Equal(t, float64(1), payload["outputTokenCount"])
			})
		}
	}
}
