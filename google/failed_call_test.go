package google

import (
	"context"
	"net/http"
	"testing"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/genai"
)

func TestFailedGenerateContentRecord_KeepsTheStatusNameButNotTheReplyText(t *testing.T) {
	t.Setenv("REVENIUM_CAPTURE_ERROR_MESSAGES", "true")
	t.Setenv("REVENIUM_CAPTURE_PROMPTS", "")
	c := newStubbedClient(t, ProviderGoogleAI, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusBadRequest, `{"error":{"code":400,"message":"API key not valid: AIzaSyFAKEFAKEFAKEFAKEFAKEFAKEFAKEFAKE1. Request was 'say hello please'","status":"INVALID_ARGUMENT"}}`)
	})

	_, err := c.client.Models().GenerateContent(context.Background(), "gemini-3.5-flash-lite", genai.Text("say hello please"), nil)
	require.Error(t, err)

	record := singleRecord(t, c)
	assert.Equal(t, "ERROR", record["stopReason"])
	assert.Equal(t, "INVALID_ARGUMENT (HTTP 400)", record["errorReason"])
	assert.Equal(t, float64(400), record["errorCode"])
}

func TestFailedVeoOperation_KeepsItsDeterministicIdsWhenTheCallerSetsAnIdempotencyKey(t *testing.T) {
	fastVideoPolling(t)
	c := newStubbedClient(t, ProviderGoogleAI, failedVeo().handle)
	ctx := core.WithUsageMetadata(context.Background(), map[string]interface{}{"idempotencyKey": "veo-job-7", "transactionId": "tx-order-42"})

	op, err := c.client.Models().GenerateVideo(ctx, geminiVeoModel, "a cat surfing", nil, nil)
	require.NoError(t, err)
	done := pollLikeGoogleDocs(t, c, op)
	require.NotNil(t, done.Error)

	record := singleRecord(t, c)
	operationID := operationTransactionID(geminiVeoOperation)
	assert.Equal(t, operationID, record["transactionId"], "a failed operation keeps the id the tracker derives from its name")
	assert.Equal(t, operationID, c.revenium.GetRequests()[0].Headers.Get("Idempotency-Key"),
		"another process reporting the same failed operation must send the same key")
	assert.Equal(t, "tx-order-42", record["traceId"])
}

func TestContentCallRefusedByGenaiBeforeAnyRequestIsNotRecorded(t *testing.T) {
	c := newStubbedClient(t, ProviderVertexAI, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, geminiNoUsageResponse)
	})
	refused := &genai.GenerateContentConfig{EnableEnhancedCivicAnswers: genai.Ptr(true)}

	_, err := c.client.Models().GenerateContent(context.Background(), "gemini-3.5-flash-lite", genai.Text("hi"), refused)
	require.Error(t, err)
	for _, err := range c.client.Models().GenerateContentStream(context.Background(), "gemini-3.5-flash-lite", genai.Text("hi"), refused) {
		require.Error(t, err)
	}

	assert.Empty(t, c.provider.requests())
	assert.Empty(t, c.records(t))
}
