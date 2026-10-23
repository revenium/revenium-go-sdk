package perplexity

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	openai "github.com/openai/openai-go/v3"

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

func sonarParams() openai.ChatCompletionNewParams {
	return openai.ChatCompletionNewParams{
		Model:    "sonar",
		Messages: []openai.ChatCompletionMessageParamUnion{openai.UserMessage("hi")},
	}
}

func onlyRecord(t *testing.T, provider http.Handler, call func(ctx context.Context, r *ReveniumPerplexity)) map[string]interface{} {
	t.Helper()
	providerServer := httptest.NewServer(provider)
	defer providerServer.Close()
	mock := testutil.NewMockMeteringServer()
	defer mock.Close()
	r := newTestPerplexity(t, providerServer.URL, mock.URL())
	defer r.Close()

	call(core.WithUsageMetadata(context.Background(), attributionMetadata()), r)
	r.Flush()

	require.True(t, mock.WaitForPayloads(1, 2*time.Second))
	payloads := mock.GetPayloads()
	require.Len(t, payloads, 1)
	return payloads[0]
}

func TestChatRecord_CarriesReportedTokensTokenLimitAndAttribution(t *testing.T) {
	provider := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"pplx-contract","object":"chat.completion","created":1700000000,"model":"sonar",` +
			`"choices":[{"index":0,"message":{"role":"assistant","content":"cut"},"finish_reason":"length"}],` +
			`"usage":{"prompt_tokens":37,"completion_tokens":11,"total_tokens":48}}`))
	})

	record := onlyRecord(t, provider, func(ctx context.Context, r *ReveniumPerplexity) {
		_, err := r.Chat().Completions().New(ctx, sonarParams())
		require.NoError(t, err)
	})

	assert.Equal(t, float64(37), record["inputTokenCount"])
	assert.Equal(t, float64(11), record["outputTokenCount"])
	assert.Equal(t, float64(48), record["totalTokenCount"])
	assert.Equal(t, "TOKEN_LIMIT", record["stopReason"])
	assertAttribution(t, record)
}

func TestFailedChatRecord_CarriesAttribution(t *testing.T) {
	provider := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"bad request","type":"invalid_request_error"}}`))
	})

	record := onlyRecord(t, provider, func(ctx context.Context, r *ReveniumPerplexity) {
		_, err := r.Chat().Completions().New(ctx, sonarParams())
		require.Error(t, err)
	})

	assert.Equal(t, "ERROR", record["stopReason"])
	assertAttribution(t, record)
}

func TestStreamedChatRecord_CarriesReportedTokensAndTokenLimit(t *testing.T) {
	provider := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		writeSSE(t, w, `data: {"id":"pplx-1","object":"chat.completion.chunk","created":1700000000,"model":"sonar","choices":[{"index":0,"delta":{"role":"assistant","content":"cut"},"finish_reason":null}]}`+"\n\n")
		writeSSE(t, w, `data: {"id":"pplx-1","object":"chat.completion.chunk","created":1700000000,"model":"sonar","choices":[{"index":0,"delta":{},"finish_reason":"length"}],"usage":{"prompt_tokens":37,"completion_tokens":11,"total_tokens":48}}`+"\n\n")
		writeSSE(t, w, "data: [DONE]\n\n")
	})

	record := onlyRecord(t, provider, func(ctx context.Context, r *ReveniumPerplexity) {
		stream, err := r.Chat().Completions().NewStreaming(ctx, sonarParams())
		require.NoError(t, err)
		for stream.Next() {
			_ = stream.Current()
		}
		require.NoError(t, stream.Close())
	})

	assert.Equal(t, float64(37), record["inputTokenCount"])
	assert.Equal(t, float64(11), record["outputTokenCount"])
	assert.Equal(t, "TOKEN_LIMIT", record["stopReason"])
	assertAttribution(t, record)
}

func TestStreaming_StopReasonIsChoiceZerosWhereverItSits(t *testing.T) {
	twoChoices := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(`data: {"id":"p1","object":"chat.completion.chunk","created":1700000000,"model":"sonar","choices":[{"index":1,"delta":{"content":"Lyon"},"finish_reason":"length"},{"index":0,"delta":{"content":"Paris"},"finish_reason":"stop"}]}` + "\n\n" +
			`data: {"id":"p1","object":"chat.completion.chunk","created":1700000000,"model":"sonar","choices":[{"index":1,"delta":{},"finish_reason":"length"}]}` + "\n\n" +
			"data: [DONE]\n\n"))
	})

	record := onlyRecord(t, twoChoices, func(ctx context.Context, r *ReveniumPerplexity) {
		stream, err := r.Chat().Completions().NewStreaming(ctx, sonarParams())
		require.NoError(t, err)
		for stream.Next() {
		}
		require.NoError(t, stream.Err())
		require.NoError(t, stream.Close())
	})

	assert.Equal(t, "END", record["stopReason"])
}
