package ollama

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/revenium/revenium-go-sdk/core/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFailedChatRecord_CarriesAttribution(t *testing.T) {
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"bad request"}`))
	}))
	defer provider.Close()
	mock := testutil.NewMockMeteringServer()
	defer mock.Close()
	r := newTestOllama(t, provider.URL, mock.URL())
	defer r.Close()
	ctx := core.WithUsageMetadata(context.Background(), attributionMetadata())

	_, err := r.ChatCompletions(ctx, ChatCompletionRequest{Model: "llama3", Messages: []ChatMessage{{Role: "user", Content: "hi"}}})
	require.Error(t, err)
	r.Flush()

	require.True(t, mock.WaitForPayloads(1, 2*time.Second))
	record := mock.GetPayloads()[0]
	assert.Equal(t, "ERROR", record["stopReason"])
	assertAttribution(t, record)
}
