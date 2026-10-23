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

func attributionMetadata() map[string]interface{} {
	return map[string]interface{}{
		"organizationName": "org-contract",
		"productName":      "product-contract",
		"agent":            "agent-contract",
		"taskType":         "task-contract",
		"traceId":          "trace-contract",
		"subscriptionId":   "subscription-contract",
	}
}

func assertAttribution(t *testing.T, record map[string]interface{}) {
	t.Helper()
	for field, want := range attributionMetadata() {
		assert.Equal(t, want, record[field], "record field %q", field)
	}
}

func TestChatRecord_CarriesReportedTokensTokenLimitAndAttribution(t *testing.T) {
	providerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl-contract","object":"chat.completion","created":1700000000,"model":"llama3",` +
			`"choices":[{"index":0,"message":{"role":"assistant","content":"cut"},"finish_reason":"length"}],` +
			`"usage":{"prompt_tokens":37,"completion_tokens":11,"total_tokens":48}}`))
	}))
	defer providerServer.Close()
	mock := testutil.NewMockMeteringServer()
	defer mock.Close()

	r := newSubscriberTestOllama(t, providerServer.URL, mock.URL())
	defer r.Close()
	ctx := core.WithUsageMetadata(context.Background(), attributionMetadata())

	_, err := r.ChatCompletions(ctx, ChatCompletionRequest{Model: "llama3", Messages: []ChatMessage{{Role: "user", Content: "hi"}}})
	require.NoError(t, err)
	r.Flush()

	require.True(t, mock.WaitForPayloads(1, 2*time.Second))
	record := mock.GetPayloads()[0]
	assert.Equal(t, float64(37), record["inputTokenCount"])
	assert.Equal(t, float64(11), record["outputTokenCount"])
	assert.Equal(t, float64(48), record["totalTokenCount"])
	assert.Equal(t, "TOKEN_LIMIT", record["stopReason"])
	assertAttribution(t, record)
}
