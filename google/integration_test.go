//go:build integration

package google

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/genai"
)

func setupIntegration(t *testing.T) {
	t.Helper()
	Reset()
	err := Initialize(WithDebug(true))
	require.NoError(t, err)
}

func TestIntegration_CreateEmbedding(t *testing.T) {
	setupIntegration(t)

	client, err := GetClient()
	require.NoError(t, err)
	defer client.Flush()

	resp, err := client.Models().CreateEmbedding(
		context.Background(),
		"text-embedding-004",
		[]*genai.Content{genai.NewContentFromText("Hello world", "user")},
		nil,
	)

	if err != nil {
		t.Logf("CreateEmbedding error (API key may lack permissions): %v", err)
		t.Skip("Embedding API not available with current credentials")
	}
	assert.NotEmpty(t, resp.Embeddings)
	assert.NotEmpty(t, resp.Embeddings[0].Values)
	t.Logf("Embedding dimensions: %d", len(resp.Embeddings[0].Values))
	if resp.Metadata != nil {
		t.Logf("Billable characters: %d", resp.Metadata.BillableCharacterCount)
	}
}

func TestIntegration_GenerateContent_WithConfidenceScore(t *testing.T) {
	setupIntegration(t)

	client, err := GetClient()
	require.NoError(t, err)
	defer client.Flush()

	resp, err := client.Models().GenerateContent(
		context.Background(),
		"gemini-2.0-flash",
		[]*genai.Content{genai.NewContentFromText("What is 2+2? Answer in one word.", "user")},
		nil,
	)

	require.NoError(t, err)
	assert.NotEmpty(t, resp.Candidates)

	score := ExtractConfidenceScore(resp)
	if score != nil {
		t.Logf("Confidence score: %f", *score)
	} else {
		t.Log("No confidence score available (expected for non-grounded requests)")
	}
}

func TestIntegration_GenerateContentStream(t *testing.T) {
	setupIntegration(t)

	client, err := GetClient()
	require.NoError(t, err)
	defer client.Flush()

	stream := client.Models().GenerateContentStream(
		context.Background(),
		"gemini-2.0-flash",
		[]*genai.Content{genai.NewContentFromText("Count from 1 to 5", "user")},
		nil,
	)

	chunks := 0
	for resp, err := range stream {
		require.NoError(t, err)
		assert.NotNil(t, resp)
		chunks++
	}
	assert.Greater(t, chunks, 0)
	t.Logf("Received %d chunks", chunks)
}

func TestIntegration_GenerateImage(t *testing.T) {
	setupIntegration(t)

	client, err := GetClient()
	require.NoError(t, err)
	defer client.Flush()

	resp, err := client.Models().GenerateImage(
		context.Background(),
		"imagen-3.0-generate-002",
		"A simple red circle on white background",
		&genai.GenerateImagesConfig{
			NumberOfImages: 1,
		},
	)

	if err != nil {
		t.Logf("GenerateImage error (may require Vertex AI): %v", err)
		t.Skip("Image generation may require Vertex AI backend")
	}

	assert.NotEmpty(t, resp.GeneratedImages)
	t.Logf("Generated %d images", len(resp.GeneratedImages))
}

func TestIntegration_GenerateVideo(t *testing.T) {
	setupIntegration(t)

	client, err := GetClient()
	require.NoError(t, err)
	defer client.Flush()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	models := client.Models()

	operation, err := models.GenerateVideo(
		ctx,
		"veo-2.0-generate-001",
		"A calm ocean wave",
		nil,
		&genai.GenerateVideosConfig{
			NumberOfVideos: 1,
		},
	)

	if err != nil {
		t.Logf("GenerateVideo error (may require Vertex AI): %v", err)
		t.Skip("Video generation may require Vertex AI backend")
	}

	result, err := models.WaitForVideo(ctx, "veo-2.0-generate-001", operation, "generation", &genai.GenerateVideosConfig{
		NumberOfVideos: 1,
	})

	if err != nil {
		t.Logf("WaitForVideo error: %v", err)
		t.Skip("Video polling failed")
	}

	assert.True(t, result.Done)
	if result.Response != nil {
		t.Logf("Generated %d videos", len(result.Response.GeneratedVideos))
	}
}
