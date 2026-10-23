package litellm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/revenium/revenium-go-sdk/core/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStreamingResponse_ClosedTwiceSendsOneRecord(t *testing.T) {
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, err := w.Write([]byte(
			"data: {\"id\":\"resp-close-twice\",\"model\":\"openai/gpt-4.1-nano\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hi\"}}]}\n\n" +
				"data: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":21,\"completion_tokens\":3,\"total_tokens\":24}}\n\n" +
				"data: [DONE]\n\n"))
		assert.NoError(t, err)
	}))
	defer proxy.Close()

	mock := testutil.NewMockMeteringServer()
	defer mock.Close()

	r := newTestLiteLLM(t, proxy.URL, mock.URL())
	defer r.Close()

	stream, err := r.Chat().Completions().NewStreaming(context.Background(), ChatCompletionRequest{
		Model:    "openai/gpt-4.1-nano",
		Messages: []ChatMessage{{Role: "user", Content: "hello"}},
	})
	require.NoError(t, err)
	for stream.Next() {
	}
	require.NoError(t, stream.Err())

	require.NoError(t, stream.Close())
	require.NoError(t, stream.Close())
	r.Flush()

	require.True(t, mock.WaitForPayloads(1, 2*time.Second))
	time.Sleep(200 * time.Millisecond)
	payloads := mock.GetPayloads()
	require.Len(t, payloads, 1, "one streamed call must produce exactly one usage record")
	assert.Equal(t, float64(21), payloads[0]["inputTokenCount"])
}
