package google

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/revenium/revenium-go-sdk/core/metering"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/genai"
)

const imagenModel = "imagen-4.0-generate-001"

func imagenPredictions(w http.ResponseWriter, r *http.Request) {
	if !strings.HasSuffix(r.URL.Path, ":predict") {
		http.NotFound(w, r)
		return
	}
	writeJSON(w, http.StatusOK, `{"predictions":[{"bytesBase64Encoded":"aGk=","mimeType":"image/png"}]}`)
}

func TestVertexImagenRecord_CarriesCatalogIdentityAndTopLevelFields(t *testing.T) {
	c := newStubbedClient(t, ProviderVertexAI, imagenPredictions)

	_, err := c.client.Models().GenerateImage(context.Background(), imagenModel, "a cat", &genai.GenerateImagesConfig{NumberOfImages: 1, AspectRatio: "16:9"})
	require.NoError(t, err)

	record := singleRecord(t, c)
	assert.Equal(t, "vertex_ai-image-models", record["provider"])
	assert.Equal(t, "vertex_ai/"+imagenModel, record["model"])
	assert.Equal(t, "PER_IMAGE", record["billingUnit"])
	assert.Equal(t, "1024x576", record["resolution"])
	assert.Equal(t, float64(1), record["actualImageCount"])
	assert.NotContains(t, record, "attributes")
}

func TestVertexImagenUpscaleRecord_CarriesCatalogIdentity(t *testing.T) {
	c := newStubbedClient(t, ProviderVertexAI, imagenPredictions)

	_, err := c.client.Models().UpscaleImage(context.Background(), "imagen-4.0-upscale-preview", &genai.Image{ImageBytes: []byte("hi"), MIMEType: "image/png"}, "x2", &genai.UpscaleImageConfig{})
	require.NoError(t, err)

	record := singleRecord(t, c)
	assert.Equal(t, "vertex_ai-image-models", record["provider"])
	assert.Equal(t, "vertex_ai/imagen-4.0-upscale-preview", record["model"])
	assert.Equal(t, "upscale", record["operationSubtype"])
	assert.Equal(t, "PER_IMAGE", record["billingUnit"])
	assert.NotContains(t, record, "attributes")
}

func TestGeminiImagenRefusedByGenaiBeforeAnyRequestIsNotRecorded(t *testing.T) {
	c := newStubbedClient(t, ProviderGoogleAI, imagenPredictions)

	_, err := c.client.Models().GenerateImage(context.Background(), imagenModel, "a cat", nil)
	require.Error(t, err, "genai refuses Imagen on the Gemini API before any request")

	assert.Empty(t, c.provider.requests())
	assert.Empty(t, c.records(t))
}

func TestImageCallThatReachedGoogleAndFailedIsRecordedAsError(t *testing.T) {
	c := newStubbedClient(t, ProviderVertexAI, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusTooManyRequests, `{"error":{"code":429,"message":"quota","status":"RESOURCE_EXHAUSTED"}}`)
	})

	_, err := c.client.Models().GenerateImage(context.Background(), imagenModel, "a cat", nil)
	require.Error(t, err)

	record := singleRecord(t, c)
	assert.Equal(t, "vertex_ai-image-models", record["provider"])
	assert.Equal(t, "ERROR", record["stopReason"])
}

func TestVeoRecord_CarriesCatalogIdentityAndTopLevelFields(t *testing.T) {
	cases := []struct {
		provider     Provider
		model        string
		stub         *veoStub
		wantProvider string
		wantModel    string
	}{
		{ProviderGoogleAI, geminiVeoModel, geminiVeo(1), "gemini", geminiVeoModel},
		{ProviderVertexAI, vertexVeoModel, vertexVeo(), "vertex_ai-video-models", "vertex_ai/" + vertexVeoModel},
	}
	for _, tc := range cases {
		t.Run(tc.provider.String(), func(t *testing.T) {
			fastVideoPolling(t)
			c := newStubbedClient(t, tc.provider, tc.stub.handle)
			config := &genai.GenerateVideosConfig{AspectRatio: "16:9", Resolution: "1080p"}

			op, err := c.client.Models().GenerateVideo(context.Background(), tc.model, "a cat surfing", nil, config)
			require.NoError(t, err)
			_, err = c.client.Models().WaitForVideo(context.Background(), tc.model, op, "", config)
			require.NoError(t, err)

			record := singleRecord(t, c)
			assert.Equal(t, tc.wantProvider, record["provider"])
			assert.Equal(t, tc.wantModel, record["model"])
			assert.Equal(t, "PER_SECOND", record["billingUnit"])
			assert.Equal(t, "1080p", record["resolution"])
			assert.Equal(t, float64(veoDefaultClipSeconds), record["durationSeconds"])
			assert.NotContains(t, record, "attributes")
		})
	}
}

func TestMediaIdentity_MatchesTheCatalogSpelling(t *testing.T) {
	cases := []struct {
		provider     Provider
		op           metering.OperationType
		model        string
		wantProvider string
		wantModel    string
	}{
		{ProviderGoogleAI, metering.OperationImage, imagenModel, "gemini", imagenModel},
		{ProviderGoogleAI, metering.OperationVideo, "veo-2.0-generate-001", "gemini", "veo-2.0-generate-001"},
		{ProviderVertexAI, metering.OperationImage, imagenModel, "vertex_ai-image-models", "vertex_ai/" + imagenModel},
		{ProviderVertexAI, metering.OperationVideo, "veo-3.0-generate-001", "vertex_ai-video-models", "vertex_ai/veo-3.0-generate-001"},
		{ProviderVertexAI, metering.OperationImage, "vertex_ai/" + imagenModel, "vertex_ai-image-models", "vertex_ai/" + imagenModel},
	}
	for _, tc := range cases {
		provider, model := tc.provider.mediaIdentity(tc.op, tc.model)
		assert.Equal(t, tc.wantProvider, provider)
		assert.Equal(t, tc.wantModel, model)
	}
}
