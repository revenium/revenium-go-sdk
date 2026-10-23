package grok

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/revenium/revenium-go-sdk/core/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStreamingRequestIsRejectedBeforeReachingProvider(t *testing.T) {
	var providerHits atomic.Int32
	providerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		providerHits.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"id\":\"chatcmpl-stream\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hi\"}}]}\n\ndata: [DONE]\n\n"))
	}))
	defer providerServer.Close()
	mock := testutil.NewMockMeteringServer()
	defer mock.Close()

	r := newTestGrok(t, providerServer.URL, mock.URL())
	defer r.Close()

	resp, err := r.ChatCompletions(context.Background(), ChatCompletionRequest{
		Model:    "grok-3-mini",
		Messages: []ChatMessage{{Role: "user", Content: "hi"}},
		Stream:   true,
	})
	r.Flush()

	assert.Nil(t, resp)
	require.ErrorIs(t, err, ErrStreamingNotSupported)
	assert.True(t, core.IsValidationError(err))
	assert.Contains(t, err.Error(), "streaming is not supported")
	assert.Zero(t, providerHits.Load(), "a streaming request must not reach the provider")
	assert.False(t, mock.WaitForPayloads(1, 200*time.Millisecond), "nothing was called, so nothing is recorded")
}
