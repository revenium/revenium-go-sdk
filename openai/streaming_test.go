package openai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	openaisdk "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/shared/constant"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/revenium/revenium-go-sdk/core/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestOpenAI(t *testing.T, openaiURL, meteringURL string) *ReveniumOpenAI {
	t.Helper()
	cfg := &Config{
		OpenAIAPIKey: "sk-test",
		BaseURL:      openaiURL,
		Revenium: &core.ReveniumConfig{
			APIKey:  "hak_test_key_123",
			BaseURL: meteringURL,
		},
	}
	r, err := NewReveniumOpenAI(cfg)
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
		writeSSE(t, w, `data: {"id":"chatcmpl-1","object":"chat.completion.chunk","created":1700000000,"model":"gpt-4o-mini","choices":[{"index":0,"delta":{"role":"assistant","content":"hello"},"finish_reason":null}]}`+"\n\n")
		writeSSE(t, w, `data: {"id":"chatcmpl-1","object":"chat.completion.chunk","created":1700000000,"model":"gpt-4o-mini","choices":[{"index":0,"delta":{"content":" world"},"finish_reason":null}]}`+"\n\n")
		writeSSE(t, w, `data: {"id":"chatcmpl-1","object":"chat.completion.chunk","created":1700000000,"model":"gpt-4o-mini","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":11,"completion_tokens":2,"total_tokens":13}}`+"\n\n")
		writeSSE(t, w, "data: [DONE]\n\n")
	})
}

func TestStreaming_FullCycle(t *testing.T) {
	openaiServer := httptest.NewServer(sseChatHandler(t))
	defer openaiServer.Close()

	mock := testutil.NewMockMeteringServer()
	defer mock.Close()

	r := newTestOpenAI(t, openaiServer.URL, mock.URL())
	defer r.Close()

	ctx := core.WithUsageMetadata(context.Background(), map[string]interface{}{
		"traceId": "stream-trace-1",
	})

	stream, err := r.Chat().Completions().NewStreaming(ctx, openaisdk.ChatCompletionNewParams{
		Model: "gpt-4o-mini",
		Messages: []openaisdk.ChatCompletionMessageParamUnion{
			{OfUser: &openaisdk.ChatCompletionUserMessageParam{
				Role:    constant.ValueOf[constant.User](),
				Content: openaisdk.ChatCompletionUserMessageParamContentUnion{OfString: openaisdk.String("Say hi")},
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
	assert.Equal(t, "hello world", content)

	require.True(t, mock.WaitForPayloads(1, 2*time.Second))
	payloads := mock.GetPayloads()
	require.Len(t, payloads, 1)
	p := payloads[0]
	assert.Equal(t, "CHAT", p["operationType"])
	assert.Equal(t, "gpt-4o-mini", p["model"])
	assert.Equal(t, true, p["isStreamed"])
	assert.Equal(t, float64(11), p["inputTokenCount"])
	assert.Equal(t, float64(2), p["outputTokenCount"])
	assert.Equal(t, float64(13), p["totalTokenCount"])
	assert.Equal(t, "END", p["stopReason"])
	assert.Equal(t, "stream-trace-1", p["traceId"])
}
