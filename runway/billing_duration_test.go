package runway

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/revenium/revenium-go-sdk/core/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newStubbedRunway(t *testing.T) (*ReveniumRunway, *testutil.MockMeteringServer) {
	t.Helper()
	runwayServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasPrefix(r.URL.Path, "/v1/tasks/") {
			_, _ = w.Write([]byte(`{"id":"task-1","status":"SUCCEEDED","output":["https://x/out.mp4"],"createdAt":"2026-10-08T00:00:00Z"}`))
			return
		}
		_, _ = w.Write([]byte(`{"id":"task-1"}`))
	}))
	t.Cleanup(runwayServer.Close)

	mock := testutil.NewMockMeteringServer()
	t.Cleanup(mock.Close)

	client, err := NewReveniumRunway(&Config{
		RunwayAPIKey:   "test-key",
		RunwayBaseURL:  runwayServer.URL,
		RunwayVersion:  "2024-11-06",
		RequestTimeout: 10 * time.Second,
		Revenium:       &core.ReveniumConfig{APIKey: "hak_test_key_123", BaseURL: mock.URL()},
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })
	return client, mock
}

func meteredSeconds(t *testing.T, client *ReveniumRunway, mock *testutil.MockMeteringServer) (float64, float64) {
	t.Helper()
	client.Flush()
	require.True(t, mock.WaitForPayloads(1, 2*time.Second))
	payload := mock.GetPayloads()[0]
	require.Equal(t, "VIDEO", payload["operationType"])
	billed, _ := payload["durationSeconds"].(float64)
	requested, _ := payload["requestedDurationSeconds"].(float64)
	return billed, requested
}

func TestImageToVideo_BillsRequestedLength(t *testing.T) {
	for _, seconds := range []int{5, 10} {
		t.Run(time.Duration(seconds*int(time.Second)).String(), func(t *testing.T) {
			client, mock := newStubbedRunway(t)
			_, err := client.ImageToVideo(context.Background(), &ImageToVideoRequest{PromptImage: "https://x/in.png", Model: "gen4_turbo", Duration: seconds}, nil)
			require.NoError(t, err)

			billed, requested := meteredSeconds(t, client, mock)
			assert.Equal(t, float64(seconds), billed)
			assert.Equal(t, float64(seconds), requested)
		})
	}
}

func TestImageToVideo_OmittedLengthUsesModelDefault(t *testing.T) {
	tests := []struct {
		model string
		want  float64
	}{
		{"gen4_turbo", 10},
		{"veo3.1_fast", 8},
	}
	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			client, mock := newStubbedRunway(t)
			_, err := client.ImageToVideo(context.Background(), &ImageToVideoRequest{PromptImage: "https://x/in.png", Model: tt.model}, nil)
			require.NoError(t, err)

			billed, requested := meteredSeconds(t, client, mock)
			assert.Equal(t, tt.want, billed)
			assert.Equal(t, tt.want, requested)
		})
	}
}

func TestVideoToVideo_BillsRequestedLength(t *testing.T) {
	client, mock := newStubbedRunway(t)
	_, err := client.VideoToVideo(context.Background(), &VideoToVideoRequest{PromptVideo: "https://x/in.mp4", Model: "seedance2", Duration: 10}, nil)
	require.NoError(t, err)

	billed, requested := meteredSeconds(t, client, mock)
	assert.Equal(t, float64(10), billed)
	assert.Equal(t, float64(10), requested)
}

func TestUpscaleVideo_BillsSourceLength(t *testing.T) {
	client, mock := newStubbedRunway(t)
	_, err := client.UpscaleVideo(context.Background(), &VideoUpscaleRequest{PromptVideo: "https://x/in.mp4", SourceDurationSeconds: 10}, nil)
	require.NoError(t, err)

	billed, requested := meteredSeconds(t, client, mock)
	assert.Equal(t, float64(10), billed)
	assert.Equal(t, float64(10), requested)
}

func TestUpscaleVideo_WithoutSourceLengthIsRefusedBeforeAnyCall(t *testing.T) {
	runwayCalls := 0
	runwayServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		runwayCalls++
		_, _ = w.Write([]byte(`{"id":"task-1"}`))
	}))
	defer runwayServer.Close()

	mock := testutil.NewMockMeteringServer()
	defer mock.Close()

	client, err := NewReveniumRunway(&Config{
		RunwayAPIKey:   "test-key",
		RunwayBaseURL:  runwayServer.URL,
		RequestTimeout: 10 * time.Second,
		Revenium:       &core.ReveniumConfig{APIKey: "hak_test_key_123", BaseURL: mock.URL()},
	})
	require.NoError(t, err)

	for _, seconds := range []float64{0, -1, math.NaN(), math.Inf(1)} {
		result, err := client.UpscaleVideo(context.Background(), &VideoUpscaleRequest{PromptVideo: "https://x/in.mp4", SourceDurationSeconds: seconds}, nil)
		require.ErrorIs(t, err, ErrMissingSourceDuration, "seconds=%v", seconds)
		assert.Nil(t, result)
	}
	result, err := client.UpscaleVideo(context.Background(), &VideoUpscaleRequest{PromptVideo: "https://x/in.mp4"}, nil)
	require.ErrorIs(t, err, ErrMissingSourceDuration)
	assert.True(t, core.IsValidationError(err))
	assert.Contains(t, err.Error(), "SourceDurationSeconds")
	assert.Nil(t, result)

	require.NoError(t, client.Close())
	assert.Zero(t, runwayCalls)
	assert.Empty(t, mock.GetRequests())
}

func TestUpscaleVideo_SourceLengthIsNotSentToRunway(t *testing.T) {
	var body map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&body)
		_, _ = w.Write([]byte(`{"id":"task-1"}`))
	}))
	defer server.Close()

	_, err := NewRunwayClient(&Config{RunwayAPIKey: "k", RunwayBaseURL: server.URL, RequestTimeout: time.Second}).
		CreateVideoUpscale(context.Background(), &VideoUpscaleRequest{PromptVideo: "https://x/in.mp4", SourceDurationSeconds: 10})
	require.NoError(t, err)
	assert.Equal(t, map[string]interface{}{"promptVideo": "https://x/in.mp4"}, body)
}

func TestBilledVideoSeconds(t *testing.T) {
	assert.Equal(t, float64(7), billedVideoSeconds("gen4_turbo", 7, 0))
	assert.Equal(t, float64(7), billedVideoSeconds("gen4_aleph", 7, 12), "an explicit length wins over the source length")
	assert.Equal(t, float64(12), billedVideoSeconds("gen4_aleph", 0, 12))
	assert.Equal(t, float64(10), billedVideoSeconds("gen3a_turbo", 0, 0))
	assert.Equal(t, float64(8), billedVideoSeconds("veo3", 0, 0))
	assert.Zero(t, billedVideoSeconds("unknown_model", 0, 0))
}

func TestVideoToVideo_AlephWithoutALengthIsNotBilledAGuessedLength(t *testing.T) {
	client, mock := newStubbedRunway(t)
	_, err := client.VideoToVideo(context.Background(), &VideoToVideoRequest{PromptVideo: "https://x/in.mp4", Model: "gen4_aleph"}, nil)
	require.NoError(t, err)

	billed, requested := meteredSeconds(t, client, mock)
	record := mock.GetPayloads()[0]
	assert.Zero(t, billed)
	assert.Zero(t, requested)
	assert.Contains(t, record, "durationSeconds", "the video endpoint rejects a record with no length")
	assert.Equal(t, "ERROR", record["stopReason"])
	assert.Contains(t, record["errorReason"], "video_length_unknown")
	assert.Contains(t, record["errorReason"], "gen4_aleph")
	assert.Equal(t, "task-1", record["transactionId"])
}

func TestVideoToVideo_AlephBillsTheSourceLength(t *testing.T) {
	for _, seconds := range []float64{5, 20} {
		t.Run(fmt.Sprint(seconds), func(t *testing.T) {
			client, mock := newStubbedRunway(t)
			_, err := client.VideoToVideo(context.Background(), &VideoToVideoRequest{PromptVideo: "https://x/in.mp4", Model: "gen4_aleph", SourceDurationSeconds: seconds}, nil)
			require.NoError(t, err)

			billed, requested := meteredSeconds(t, client, mock)
			assert.Equal(t, seconds, billed)
			assert.Equal(t, seconds, requested)
			assert.Equal(t, "END", mock.GetPayloads()[0]["stopReason"])
		})
	}
}

func TestImageToVideo_ModelOutsideTheDefaultsTableWithoutALengthIsNotBilledAGuessedLength(t *testing.T) {
	client, mock := newStubbedRunway(t)
	_, err := client.ImageToVideo(context.Background(), &ImageToVideoRequest{PromptImage: "https://x/in.png", Model: "gen5_preview"}, nil)
	require.NoError(t, err)

	billed, _ := meteredSeconds(t, client, mock)
	assert.Zero(t, billed)
	assert.Contains(t, mock.GetPayloads()[0]["errorReason"], "video_length_unknown")
}

func TestVideoToVideo_SourceLengthIsNotSentToRunway(t *testing.T) {
	var body map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&body)
		_, _ = w.Write([]byte(`{"id":"task-1"}`))
	}))
	defer server.Close()

	_, err := NewRunwayClient(&Config{RunwayAPIKey: "k", RunwayBaseURL: server.URL, RequestTimeout: time.Second}).
		CreateVideoToVideo(context.Background(), &VideoToVideoRequest{PromptVideo: "https://x/in.mp4", Model: "gen4_aleph", SourceDurationSeconds: 10})
	require.NoError(t, err)
	assert.Equal(t, map[string]interface{}{"promptVideo": "https://x/in.mp4", "model": "gen4_aleph"}, body)
}
