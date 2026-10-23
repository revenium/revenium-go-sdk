package google

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/genai"
)

const (
	vertexPredictEmbeddingResponse      = `{"predictions":[{"embeddings":{"values":[0.1,0.2],"statistics":{"token_count":7,"truncated":false}}}],"metadata":{"billableCharacterCount":25}}`
	vertexPredictBatchEmbeddingResponse = `{"predictions":[{"embeddings":{"values":[0.1],"statistics":{"token_count":7,"truncated":false}}},{"embeddings":{"values":[0.2],"statistics":{"token_count":5,"truncated":false}}},{"embeddings":{"values":[0.3],"statistics":{"token_count":4,"truncated":false}}}],"metadata":{"billableCharacterCount":61}}`
	vertexEmbedContentResponse          = `{"embedding":{"values":[0.1,0.2]},"usageMetadata":{"promptTokenCount":9,"promptTokensDetails":[{"modality":"TEXT","tokenCount":9}]},"truncated":false}`
	geminiBatchEmbeddingResponse        = `{"embeddings":[{"values":[0.1,0.2]}]}`
)

func embedOnStub(t *testing.T, provider Provider, model, response string, inputs ...string) map[string]interface{} {
	t.Helper()
	c := newStubbedClient(t, provider, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, response)
	})
	contents := make([]*genai.Content, 0, len(inputs))
	for _, input := range inputs {
		contents = append(contents, genai.NewContentFromText(input, genai.RoleUser))
	}

	_, err := c.client.Models().CreateEmbedding(context.Background(), model, contents, nil)
	require.NoError(t, err)

	return singleRecord(t, c)
}

func assertEmbeddingTokens(t *testing.T, record map[string]interface{}, tokens float64) {
	t.Helper()
	assert.Equal(t, "EMBED", record["operationType"])
	assert.Equal(t, tokens, record["inputTokenCount"])
	assert.Equal(t, tokens, record["totalTokenCount"])
	assert.Equal(t, float64(0), record["outputTokenCount"])
}

func TestVertexEmbeddingRecordsReportedTokenCount(t *testing.T) {
	for _, model := range []string{"text-embedding-005", "gemini-embedding-001"} {
		t.Run(model, func(t *testing.T) {
			record := embedOnStub(t, ProviderVertexAI, model, vertexPredictEmbeddingResponse, "The quick brown fox jumps")

			assertEmbeddingTokens(t, record, 7)
			assert.Equal(t, "VERTEX_AI", record["provider"])
		})
	}
}

func TestVertexEmbeddingBatchSumsTokenCounts(t *testing.T) {
	record := embedOnStub(t, ProviderVertexAI, "text-embedding-005", vertexPredictBatchEmbeddingResponse, "first input", "second", "third")

	assertEmbeddingTokens(t, record, 16)
}

func TestVertexEmbedContentModelRecordsPromptTokenCount(t *testing.T) {
	record := embedOnStub(t, ProviderVertexAI, "gemini-embedding-2", vertexEmbedContentResponse, "The quick brown fox jumps")

	assertEmbeddingTokens(t, record, 9)
}

func TestGeminiAPIEmbeddingWithoutTokenCountsRecordsZeroTokens(t *testing.T) {
	record := embedOnStub(t, ProviderGoogleAI, "gemini-embedding-001", geminiBatchEmbeddingResponse, "The quick brown fox jumps")

	assertEmbeddingTokens(t, record, 0)
}

func TestReadmeSaysGeminiAPIEmbeddingsAreRecordedWithoutTokens(t *testing.T) {
	readme, err := os.ReadFile(filepath.Join("..", "README.md"))
	require.NoError(t, err)

	assert.Contains(t, strings.Join(strings.Fields(string(readme)), " "),
		"Embeddings on the Gemini API (`GOOGLE_API_KEY`) are recorded with zero tokens, so they show no cost in Revenium")
}
