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

const proxyTransactionName = "checkout"

func newTransactionNamingProxy(t *testing.T, contentType, body string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Revenium-Transaction-Name", proxyTransactionName)
		w.Header().Set("Content-Type", contentType)
		_, err := w.Write([]byte(body))
		assert.NoError(t, err)
	}))
}

func TestChatRecordCarriesTheProxyTransactionNameHeader(t *testing.T) {
	proxy := newTransactionNamingProxy(t, "application/json", attributedChatBody)
	defer proxy.Close()
	mock := testutil.NewMockMeteringServer()
	defer mock.Close()
	r := newTestLiteLLM(t, proxy.URL, mock.URL())
	defer r.Close()

	_, err := r.Chat().Completions().New(context.Background(), ChatCompletionRequest{
		Model:    "openai/gpt-4.1-nano",
		Messages: []ChatMessage{{Role: "user", Content: "hi"}},
	})
	require.NoError(t, err)
	r.Flush()

	require.True(t, mock.WaitForPayloads(1, 2*time.Second))
	assert.Equal(t, proxyTransactionName, mock.GetPayloads()[0]["transactionName"])
}

func TestStreamingRecordCarriesTheProxyTransactionNameHeader(t *testing.T) {
	proxy := newTransactionNamingProxy(t, "text/event-stream", attributedStreamBody)
	defer proxy.Close()
	mock := testutil.NewMockMeteringServer()
	defer mock.Close()
	r := newTestLiteLLM(t, proxy.URL, mock.URL())
	defer r.Close()

	stream, err := r.Chat().Completions().NewStreaming(context.Background(), ChatCompletionRequest{
		Model:    "openai/gpt-4.1-nano",
		Messages: []ChatMessage{{Role: "user", Content: "hi"}},
	})
	require.NoError(t, err)
	for stream.Next() {
	}
	require.NoError(t, stream.Err())
	require.NoError(t, stream.Close())
	r.Flush()

	require.True(t, mock.WaitForPayloads(1, 2*time.Second))
	assert.Equal(t, proxyTransactionName, mock.GetPayloads()[0]["transactionName"])
}
