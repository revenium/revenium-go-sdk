package perplexity

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	openai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/shared/constant"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/revenium/revenium-go-sdk/core/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestPerplexity(t *testing.T, providerURL, meteringURL string) *ReveniumPerplexity {
	t.Helper()
	cfg := &Config{
		PerplexityAPIKey:  "pplx-test",
		PerplexityBaseURL: providerURL,
		Revenium: &core.ReveniumConfig{
			APIKey:  "hak_test_key_123",
			BaseURL: meteringURL,
		},
	}
	r, err := NewReveniumPerplexity(cfg)
	require.NoError(t, err)
	return r
}

func writeSSE(t *testing.T, w http.ResponseWriter, line string) {
	t.Helper()
	if _, err := w.Write([]byte(line)); err != nil {
		t.Errorf("writeSSE: %v", err)
		return
	}
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}

func sseChatHandler(t *testing.T) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		writeSSE(t, w, `data: {"id":"pplx-1","object":"chat.completion.chunk","created":1700000000,"model":"sonar-small","choices":[{"index":0,"delta":{"role":"assistant","content":"hi"},"finish_reason":null}]}`+"\n\n")
		writeSSE(t, w, `data: {"id":"pplx-1","object":"chat.completion.chunk","created":1700000000,"model":"sonar-small","choices":[{"index":0,"delta":{"content":" there"},"finish_reason":null}]}`+"\n\n")
		writeSSE(t, w, `data: {"id":"pplx-1","object":"chat.completion.chunk","created":1700000000,"model":"sonar-small","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":7,"completion_tokens":2,"total_tokens":9,"cost":{"input_tokens_cost":0.0001,"output_tokens_cost":0.0002,"total_cost":0.0003}}}`+"\n\n")
		writeSSE(t, w, "data: [DONE]\n\n")
	})
}

func TestStreaming_FullCycle_Perplexity(t *testing.T) {
	pplxServer := httptest.NewServer(sseChatHandler(t))
	defer pplxServer.Close()

	mock := testutil.NewMockMeteringServer()
	defer mock.Close()

	r := newTestPerplexity(t, pplxServer.URL, mock.URL())
	defer r.Close()

	ctx := core.WithUsageMetadata(context.Background(), map[string]interface{}{
		"traceId": "perplex-trace-1",
	})

	stream, err := r.Chat().Completions().NewStreaming(ctx, openai.ChatCompletionNewParams{
		Model: "sonar-small",
		Messages: []openai.ChatCompletionMessageParamUnion{
			{OfUser: &openai.ChatCompletionUserMessageParam{
				Role:    constant.ValueOf[constant.User](),
				Content: openai.ChatCompletionUserMessageParamContentUnion{OfString: openai.String("hi")},
			}},
		},
	})
	require.NoError(t, err)

	chunks := 0
	var content string
	for stream.Next() {
		chunks++
		c := stream.Current()
		if len(c.Choices) > 0 {
			content += c.Choices[0].Delta.Content
		}
	}
	require.NoError(t, stream.Err())
	require.NoError(t, stream.Close())
	r.Flush()

	assert.GreaterOrEqual(t, chunks, 2)
	assert.Equal(t, "hi there", content)

	require.True(t, mock.WaitForPayloads(1, 2*time.Second))
	payloads := mock.GetPayloads()
	require.Len(t, payloads, 1)
	p := payloads[0]
	assert.Equal(t, "CHAT", p["operationType"])
	assert.Equal(t, "sonar-small", p["model"])
	assert.Equal(t, true, p["isStreamed"])
	assert.Equal(t, float64(7), p["inputTokenCount"])
	assert.Equal(t, float64(2), p["outputTokenCount"])
	assert.Equal(t, float64(9), p["totalTokenCount"])
	assert.Equal(t, "END", p["stopReason"])
	assert.Equal(t, "perplex-trace-1", p["traceId"])
}
