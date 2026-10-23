package openai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/openai/openai-go/v3/responses"
	"github.com/revenium/revenium-go-sdk/core/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func sseResponsesHandler(t *testing.T, events ...string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		for _, event := range events {
			writeSSE(t, w, "data: "+event+"\n\n")
		}
		writeSSE(t, w, "data: [DONE]\n\n")
	})
}

const responsesTextDelta = `{"type":"response.output_text.delta","item_id":"msg_1","output_index":0,"content_index":0,"delta":"Hi","sequence_number":1}`

const responsesCompleted = `{"type":"response.completed","sequence_number":2,"response":{"id":"resp_1","object":"response","created_at":1700000000,"model":"gpt-4.1-nano","status":"completed","output":[],"usage":{"input_tokens":9,"input_tokens_details":{"cached_tokens":0},"output_tokens":2,"output_tokens_details":{"reasoning_tokens":0},"total_tokens":11}}}`

func assertSingleRecordAfterTimeout(t *testing.T, mock *testutil.MockMeteringServer) map[string]interface{} {
	t.Helper()
	require.True(t, mock.WaitForPayloads(1, 2*time.Second))
	time.Sleep(200 * time.Millisecond)
	payloads := mock.GetPayloads()
	require.Len(t, payloads, 1, "one streamed call must produce exactly one usage record")
	return payloads[0]
}

func TestStreamingWrapper_ClosedTwiceSendsOneRecord(t *testing.T) {
	provider := &usageOnRequestProvider{}
	providerServer := httptest.NewServer(provider.handler(t))
	defer providerServer.Close()

	mock := testutil.NewMockMeteringServer()
	defer mock.Close()

	r := newTestOpenAI(t, providerServer.URL, mock.URL())
	defer r.Close()

	stream, err := r.Chat().Completions().NewStreaming(context.Background(), readmeStreamingParams())
	require.NoError(t, err)
	drainStream(t, stream)

	require.NoError(t, stream.Close())
	require.NoError(t, stream.Close())
	r.Flush()

	p := assertSingleRecordAfterTimeout(t, mock)
	assert.Equal(t, float64(13), p["inputTokenCount"])
}

func TestStreamingWrapper_ConcurrentClosesSendOneRecord(t *testing.T) {
	provider := &usageOnRequestProvider{}
	providerServer := httptest.NewServer(provider.handler(t))
	defer providerServer.Close()

	mock := testutil.NewMockMeteringServer()
	defer mock.Close()

	r := newTestOpenAI(t, providerServer.URL, mock.URL())
	defer r.Close()

	stream, err := r.Chat().Completions().NewStreaming(context.Background(), readmeStreamingParams())
	require.NoError(t, err)
	drainStream(t, stream)

	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			assert.NoError(t, stream.Close())
		}()
	}
	wg.Wait()
	r.Flush()

	assertSingleRecordAfterTimeout(t, mock)
}

func TestResponsesStreamingWrapper_ClosedTwiceSendsOneRecord(t *testing.T) {
	providerServer := httptest.NewServer(sseResponsesHandler(t, responsesTextDelta, responsesCompleted))
	defer providerServer.Close()

	mock := testutil.NewMockMeteringServer()
	defer mock.Close()

	r := newTestOpenAI(t, providerServer.URL, mock.URL())
	defer r.Close()

	stream, err := r.Responses().CreateStreaming(context.Background(), responses.ResponseNewParams{Model: "gpt-4.1-nano"})
	require.NoError(t, err)
	for stream.Next() {
		stream.Current()
	}
	require.NoError(t, stream.Err())

	require.NoError(t, stream.Close())
	require.NoError(t, stream.Close())
	r.Flush()

	p := assertSingleRecordAfterTimeout(t, mock)
	assert.Equal(t, float64(9), p["inputTokenCount"])
}
