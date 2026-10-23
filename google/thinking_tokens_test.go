package google

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/genai"
)

const (
	geminiThinkingResponse   = `{"candidates":[{"content":{"parts":[{"text":"No"}],"role":"model"},"finishReason":"STOP","index":0}],"usageMetadata":{"candidatesTokenCount":1,"promptTokenCount":12,"promptTokensDetails":[{"modality":"TEXT","tokenCount":12}],"thoughtsTokenCount":150,"totalTokenCount":163},"modelVersion":"gemini-3.5-flash-lite"}`
	geminiNoThinkingResponse = `{"candidates":[{"content":{"parts":[{"text":"No"}],"role":"model"},"finishReason":"STOP","index":0}],"usageMetadata":{"candidatesTokenCount":1,"promptTokenCount":12,"totalTokenCount":13},"modelVersion":"gemini-3.5-flash-lite"}`
)

func assertTokenCounts(t *testing.T, record map[string]interface{}, input, output, reasoning, total float64) {
	t.Helper()
	assert.Equal(t, input, record["inputTokenCount"])
	assert.Equal(t, output, record["outputTokenCount"])
	assert.Equal(t, reasoning, record["reasoningTokenCount"])
	assert.Equal(t, total, record["totalTokenCount"])
}

func TestThinkingTokensAreSentInsideOutput(t *testing.T) {
	for _, provider := range streamProviders {
		t.Run(provider.String()+" non-streamed", func(t *testing.T) {
			c := newStubbedClient(t, provider, func(w http.ResponseWriter, r *http.Request) {
				writeJSON(w, http.StatusOK, geminiThinkingResponse)
			})

			_, err := c.client.Models().GenerateContent(context.Background(), "gemini-3.5-flash-lite", genai.Text("Is 91 prime? Answer yes or no."), nil)
			require.NoError(t, err)

			assertTokenCounts(t, singleRecord(t, c), 12, 151, 150, 163)
		})
		t.Run(provider.String()+" streamed", func(t *testing.T) {
			c := newStubbedClient(t, provider, streamHandler(geminiThinkingResponse))

			drainStream(t, c)

			assertTokenCounts(t, singleRecord(t, c), 12, 151, 150, 163)
		})
	}
}

func TestOutputWithoutThinkingIsUnchanged(t *testing.T) {
	c := newStubbedClient(t, ProviderGoogleAI, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, geminiNoThinkingResponse)
	})

	_, err := c.client.Models().GenerateContent(context.Background(), "gemini-3.5-flash-lite", genai.Text("Is 91 prime?"), nil)
	require.NoError(t, err)

	assertTokenCounts(t, singleRecord(t, c), 12, 1, 0, 13)
}
