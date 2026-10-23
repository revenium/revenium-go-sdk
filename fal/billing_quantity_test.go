package fal

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/revenium/revenium-go-sdk/core/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	singleImageOutput = `{"image":{"url":"https://v3.fal.media/files/x/out.png","width":2048,"height":1536,"content_type":"image/png","file_name":"out.png","file_size":1234}}`
	videoFileOutput   = `{"video":{"url":"https://v3.fal.media/files/x/out.mp4","content_type":"video/mp4","file_name":"out.mp4","file_size":98765}}`
	minimaxOutput     = `{"audio":{"url":"https://v3.fal.media/files/x/out.mp3","content_type":"audio/mpeg","file_name":"out.mp3","file_size":4567},"duration_ms":4200}`
)

func meterFalCall(t *testing.T, endpointID, falOutput string, input map[string]interface{}) map[string]interface{} {
	t.Helper()
	falServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(falOutput))
	}))
	defer falServer.Close()

	mock := testutil.NewMockMeteringServer()
	defer mock.Close()

	client, err := NewReveniumFal(&Config{
		FalAPIKey:      "test-key",
		FalBaseURL:     falServer.URL,
		QueueBaseURL:   falServer.URL,
		RequestTimeout: 10 * time.Second,
		Revenium:       &core.ReveniumConfig{APIKey: "hak_test_key_123", BaseURL: mock.URL()},
	})
	require.NoError(t, err)
	defer client.Close()

	_, err = client.Run(context.Background(), endpointID, input, nil)
	require.NoError(t, err)
	client.Flush()
	require.True(t, mock.WaitForPayloads(1, 2*time.Second))
	return mock.GetPayloads()[0]
}

func TestRun_SingleImageEndpointsMeterOneImage(t *testing.T) {
	endpoints := []string{"fal-ai/esrgan", "fal-ai/imageutils/rembg", "fal-ai/face-swap", "fal-ai/aura-sr"}
	for _, endpoint := range endpoints {
		t.Run(endpoint, func(t *testing.T) {
			payload := meterFalCall(t, endpoint, singleImageOutput, map[string]interface{}{"image_url": "https://x/in.png"})

			assert.Equal(t, "IMAGE", payload["operationType"])
			assert.EqualValues(t, 1, payload["actualImageCount"])
			assert.EqualValues(t, 1, payload["requestedImageCount"])
			attrs, ok := payload["attributes"].(map[string]interface{})
			require.True(t, ok, "attributes missing")
			assert.EqualValues(t, 2048, attrs["width"])
			assert.EqualValues(t, 1536, attrs["height"])
		})
	}
}

func TestRun_VideoDurationFromRequestOrEndpointDefault(t *testing.T) {
	tests := []struct {
		name     string
		endpoint string
		input    map[string]interface{}
		want     float64
	}{
		{"veo3 seconds suffix", "fal-ai/veo3/fast", map[string]interface{}{"prompt": "p", "duration": "6s"}, 6},
		{"veo3 omitted uses 8s default", "fal-ai/veo3/fast", map[string]interface{}{"prompt": "p"}, 8},
		{"veo3.1 omitted uses 8s default", "fal-ai/veo3.1", map[string]interface{}{"prompt": "p"}, 8},
		{"veo2 omitted uses 5s default", "fal-ai/veo2", map[string]interface{}{"prompt": "p"}, 5},
		{"kling omitted uses 5 default", "fal-ai/kling-video/v2.5-turbo/pro/text-to-video", map[string]interface{}{"prompt": "p"}, 5},
		{"kling plain string", "fal-ai/kling-video/v2.5-turbo/pro/text-to-video", map[string]interface{}{"prompt": "p", "duration": "10"}, 10},
		{"hailuo omitted uses 6 default", "fal-ai/minimax/hailuo-02/standard/text-to-video", map[string]interface{}{"prompt": "p"}, 6},
		{"pika integer", "fal-ai/pika/v2.2/text-to-video", map[string]interface{}{"prompt": "p", "duration": 10}, 10},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payload := meterFalCall(t, tt.endpoint, videoFileOutput, tt.input)

			assert.Equal(t, "VIDEO", payload["operationType"])
			assert.EqualValues(t, tt.want, payload["durationSeconds"])
			assert.EqualValues(t, tt.want, payload["requestedDurationSeconds"])
		})
	}
}

func TestRun_SpeechDurationFromTopLevelMilliseconds(t *testing.T) {
	payload := meterFalCall(t, "fal-ai/minimax/speech-02-hd", minimaxOutput, map[string]interface{}{"text": "hello"})

	assert.Equal(t, "AUDIO", payload["operationType"])
	assert.InDelta(t, 4.2, payload["durationSeconds"], 1e-9)
}

func TestParseVideoSeconds(t *testing.T) {
	tests := []struct {
		name   string
		value  interface{}
		want   float64
		wantOK bool
	}{
		{"seconds suffix", "8s", 8, true},
		{"upper-case suffix with spaces", " 4S ", 4, true},
		{"plain string", "10", 10, true},
		{"float", float64(6), 6, true},
		{"int", 5, 5, true},
		{"auto is not a length", "auto", 0, false},
		{"zero is not a length", "0", 0, false},
		{"missing", nil, 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := parseVideoSeconds(tt.value)
			assert.Equal(t, tt.wantOK, ok)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestEndpointDefaultVideoSeconds_PinsEveryTableEntry(t *testing.T) {
	publishedDefaults := map[string]float64{
		"fal-ai/veo2":      5,
		"fal-ai/veo3/fast": 8,
		"fal-ai/kling-video/v2.5-turbo/pro/text-to-video": 5,
		"fal-ai/minimax/hailuo-02/standard/text-to-video": 6,
		"fal-ai/luma-dream-machine/ray-2":                 5,
		"fal-ai/pixverse/v5/text-to-video":                5,
		"fal-ai/bytedance/seedance/v1/pro/text-to-video":  5,
		"fal-ai/wan-25-preview/text-to-video":             5,
		"fal-ai/pika/v2.2/text-to-video":                  5,
		"fal-ai/vidu/q2/text-to-video":                    4,
		"fal-ai/ltx-2/text-to-video":                      6,
		"fal-ai/sora-2/text-to-video":                     4,
		"fal-ai/wan/v2.2-5b/text-to-video":                81.0 / 24,
		"fal-ai/wan/v2.2-a14b/text-to-video":              81.0 / 16,
		"fal-ai/wan-t2v":                                  81.0 / 16,
		"fal-ai/hunyuan-video-v1.5/text-to-video":         121.0 / 24,
		"fal-ai/hunyuan-video":                            129.0 / 24,
	}

	for endpoint, want := range publishedDefaults {
		got, ok := endpointDefaultVideoSeconds(endpoint)
		assert.True(t, ok, endpoint)
		assert.Equal(t, want, got, endpoint)
	}

	for i, entry := range defaultVideoSeconds {
		pinned := false
		for endpoint := range publishedDefaults {
			if firstMatchingDefault(endpoint) == i {
				pinned = true
				break
			}
		}
		assert.True(t, pinned, "default for %s has no endpoint in publishedDefaults", entry.pattern)
	}

	_, ok := endpointDefaultVideoSeconds("fal-ai/totally-unknown-video")
	assert.False(t, ok)
}

func firstMatchingDefault(endpointID string) int {
	for i, entry := range defaultVideoSeconds {
		if entry.pattern.MatchString(endpointID) {
			return i
		}
	}
	return -1
}
