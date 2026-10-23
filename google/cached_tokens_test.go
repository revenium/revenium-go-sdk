package google

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/genai"
)

const geminiCachedResponse = `{"candidates":[{"content":{"parts":[{"text":"Yes"}],"role":"model"},"finishReason":"STOP","index":0}],"usageMetadata":{"promptTokenCount":4949,"cachedContentTokenCount":4941,"candidatesTokenCount":1,"totalTokenCount":4950,"cacheTokensDetails":[{"modality":"TEXT","tokenCount":4941}]},"modelVersion":"gemini-3.5-flash-lite"}`

func TestCachedPromptTokensAreSentAsSubsetOfInput(t *testing.T) {
	for _, provider := range streamProviders {
		t.Run(provider.String(), func(t *testing.T) {
			c := newStubbedClient(t, provider, func(w http.ResponseWriter, r *http.Request) {
				writeJSON(w, http.StatusOK, geminiCachedResponse)
			})

			_, err := c.client.Models().GenerateContent(context.Background(), "gemini-3.5-flash-lite", genai.Text("Summarise the cached document."), nil)
			require.NoError(t, err)

			record := singleRecord(t, c)
			assert.Equal(t, float64(4949), record["inputTokenCount"], "input stays Gemini's gross promptTokenCount")
			assert.Equal(t, float64(4941), record["cacheReadTokenCount"])
			assert.Equal(t, float64(0), record["cacheCreationTokenCount"])
			assert.Equal(t, "gemini-3.5-flash-lite", record["model"])
		})
	}
}
