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

const geminiTokenLimitResponse = `{"candidates":[{"content":{"parts":[{"text":"cut"}],"role":"model"},"finishReason":"MAX_TOKENS","index":0}],` +
	`"usageMetadata":{"promptTokenCount":40,"candidatesTokenCount":11,"thoughtsTokenCount":6,"cachedContentTokenCount":25,"totalTokenCount":57},"modelVersion":"gemini-3.5-flash-lite"}`

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

func TestGenerateContentRecord_CarriesReportedTokensTokenLimitAndAttribution(t *testing.T) {
	for _, provider := range streamProviders {
		t.Run(provider.String(), func(t *testing.T) {
			c := newStubbedClient(t, provider, func(w http.ResponseWriter, r *http.Request) {
				writeJSON(w, http.StatusOK, geminiTokenLimitResponse)
			})

			_, err := c.client.Models().GenerateContent(attributedContext(), "gemini-3.5-flash-lite", genai.Text("Write a long story."), nil)
			require.NoError(t, err)

			record := singleRecord(t, c)
			assertTokenCounts(t, record, 40, 17, 6, 57)
			assert.Equal(t, float64(25), record["cacheReadTokenCount"])
			assert.Equal(t, "TOKEN_LIMIT", record["stopReason"])
			assertAttribution(t, record)
		})
	}
}

func TestFailedGenerateContentRecord_CarriesAttribution(t *testing.T) {
	c := newStubbedClient(t, ProviderGoogleAI, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusBadRequest, `{"error":{"code":400,"message":"bad request","status":"INVALID_ARGUMENT"}}`)
	})

	_, err := c.client.Models().GenerateContent(attributedContext(), "gemini-3.5-flash-lite", genai.Text("hi"), nil)
	require.Error(t, err)

	record := singleRecord(t, c)
	assert.Equal(t, "ERROR", record["stopReason"])
	assertAttribution(t, record)
}

func TestStreamedGenerateContentRecord_CarriesReportedTokensAndAttribution(t *testing.T) {
	c := newStubbedClient(t, ProviderGoogleAI, streamHandler(geminiTokenLimitResponse))

	stream := c.client.Models().GenerateContentStream(attributedContext(), "gemini-3.5-flash-lite", genai.Text("Write a long story."), nil)
	for _, err := range stream {
		require.NoError(t, err)
	}

	record := singleRecord(t, c)
	assertTokenCounts(t, record, 40, 17, 6, 57)
	assert.Equal(t, float64(25), record["cacheReadTokenCount"])
	assert.Equal(t, "TOKEN_LIMIT", record["stopReason"])
	assertAttribution(t, record)
}
