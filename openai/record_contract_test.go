package openai

import (
	"context"
	"net/http"
	"testing"

	openaisdk "github.com/openai/openai-go/v3"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const badRequestBody = `{"error":{"message":"bad request","type":"invalid_request_error"}}`

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

func attributedContext() context.Context {
	return core.WithUsageMetadata(context.Background(), attributionMetadata())
}

func assertAttribution(t *testing.T, record map[string]interface{}) {
	t.Helper()
	for field, want := range attributionMetadata() {
		assert.Equal(t, want, record[field], "record field %q", field)
	}
}

func badRequestResponder() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(badRequestBody))
	})
}

func TestStreamedChatRecord_CarriesTheProvidersTokenLimitEnding(t *testing.T) {
	provider := sseResponder(t,
		`{"id":"chatcmpl-1","object":"chat.completion.chunk","created":1700000000,"model":"gpt-4.1-nano","choices":[{"index":0,"delta":{"role":"assistant","content":"cut"},"finish_reason":"length"}]}`,
		`{"id":"chatcmpl-1","object":"chat.completion.chunk","created":1700000000,"model":"gpt-4.1-nano","choices":[],"usage":{"prompt_tokens":37,"completion_tokens":11,"total_tokens":48}}`,
	)

	record := recordedCall(t, provider, func(r *ReveniumOpenAI) error {
		stream, err := r.Chat().Completions().NewStreaming(attributedContext(), cachedChatParams())
		if err != nil {
			return err
		}
		for stream.Next() {
		}
		return stream.Close()
	})

	assert.Equal(t, "TOKEN_LIMIT", record["stopReason"])
	assert.Equal(t, float64(37), record["inputTokenCount"])
	assert.Equal(t, float64(11), record["outputTokenCount"])
	assertAttribution(t, record)
}

func TestFailedChatRecord_CarriesAttribution(t *testing.T) {
	record := recordedCall(t, badRequestResponder(), func(r *ReveniumOpenAI) error {
		_, err := r.Chat().Completions().New(attributedContext(), cachedChatParams())
		require.Error(t, err)
		return nil
	})

	assert.Equal(t, "ERROR", record["stopReason"])
	assertAttribution(t, record)
}

func TestResponsesRecord_CarriesAttribution(t *testing.T) {
	record := recordedCall(t, jsonResponder(responseJSON(plainPromptDetails)), func(r *ReveniumOpenAI) error {
		_, err := r.Responses().Create(attributedContext(), cachedResponsesParams())
		return err
	})

	assertAttribution(t, record)
}

func TestFailedResponsesRecord_CarriesAttribution(t *testing.T) {
	record := recordedCall(t, badRequestResponder(), func(r *ReveniumOpenAI) error {
		_, err := r.Responses().Create(attributedContext(), cachedResponsesParams())
		require.Error(t, err)
		return nil
	})

	assert.Equal(t, "ERROR", record["stopReason"])
	assertAttribution(t, record)
}

func TestEmbeddingRecord_CarriesReportedTokensAndAttribution(t *testing.T) {
	body := `{"object":"list","data":[{"object":"embedding","index":0,"embedding":[0.1,0.2]}],` +
		`"model":"text-embedding-3-small","usage":{"prompt_tokens":8,"total_tokens":8}}`

	record := recordedCall(t, jsonResponder(body), func(r *ReveniumOpenAI) error {
		_, err := r.Embeddings().Create(attributedContext(), openaisdk.EmbeddingNewParams{
			Model: "text-embedding-3-small",
			Input: openaisdk.EmbeddingNewParamsInputUnion{OfString: openaisdk.String("hi")},
		})
		return err
	})

	assert.Equal(t, "EMBED", record["operationType"])
	assert.Equal(t, float64(8), record["inputTokenCount"])
	assert.Equal(t, float64(0), record["outputTokenCount"])
	assert.Equal(t, float64(8), record["totalTokenCount"])
	assertAttribution(t, record)
}

func TestFailedEmbeddingRecord_CarriesAttribution(t *testing.T) {
	record := recordedCall(t, badRequestResponder(), func(r *ReveniumOpenAI) error {
		_, err := r.Embeddings().Create(attributedContext(), openaisdk.EmbeddingNewParams{
			Model: "text-embedding-3-small",
			Input: openaisdk.EmbeddingNewParamsInputUnion{OfString: openaisdk.String("hi")},
		})
		require.Error(t, err)
		return nil
	})

	assert.Equal(t, "ERROR", record["stopReason"])
	assertAttribution(t, record)
}
