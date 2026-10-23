package google

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/genai"
)

func TestEmbeddingRecord_CarriesAttribution(t *testing.T) {
	c := newStubbedClient(t, ProviderVertexAI, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, vertexPredictEmbeddingResponse)
	})

	contents := []*genai.Content{genai.NewContentFromText("The quick brown fox", genai.RoleUser)}
	_, err := c.client.Models().CreateEmbedding(attributedContext(), "text-embedding-005", contents, nil)
	require.NoError(t, err)

	record := singleRecord(t, c)
	assert.Equal(t, "EMBED", record["operationType"])
	assertAttribution(t, record)
}

func TestImageRecord_CarriesAttribution(t *testing.T) {
	c := newStubbedClient(t, ProviderVertexAI, imagenPredictions)

	_, err := c.client.Models().GenerateImage(attributedContext(), imagenModel, "a cat", nil)
	require.NoError(t, err)

	record := singleRecord(t, c)
	assert.Equal(t, float64(1), record["actualImageCount"])
	assertAttribution(t, record)
}
