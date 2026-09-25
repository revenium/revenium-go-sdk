package fal

import (
	"testing"
	"time"

	"github.com/revenium/revenium-go-sdk/core/metering"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildPayloadFromResult_OperationSubtype(t *testing.T) {
	tests := []struct {
		name     string
		endpoint string
		op       metering.OperationType
		result   map[string]interface{}
		metadata map[string]interface{}
		want     string
	}{
		{
			name:     "image defaults to generation",
			endpoint: "fal-ai/flux/schnell",
			op:       metering.OperationImage,
			result:   map[string]interface{}{"images": []interface{}{map[string]interface{}{"url": "https://x/i.png", "width": float64(512), "height": float64(512)}}},
			want:     metering.SubtypeGeneration,
		},
		{
			name:     "image upscale endpoint",
			endpoint: "fal-ai/clarity-upscaler",
			op:       metering.OperationImage,
			result:   map[string]interface{}{"images": []interface{}{}},
			want:     metering.SubtypeUpscale,
		},
		{
			name:     "video defaults to generation",
			endpoint: "fal-ai/kling-video/v1/standard/text-to-video",
			op:       metering.OperationVideo,
			result:   map[string]interface{}{"video": map[string]interface{}{"url": "https://x/v.mp4", "duration": 5.0}},
			want:     metering.SubtypeGeneration,
		},
		{
			name:     "video extend endpoint",
			endpoint: "fal-ai/luma-dream-machine/ray-2/extend",
			op:       metering.OperationVideo,
			result:   map[string]interface{}{"video": map[string]interface{}{"url": "https://x/v.mp4", "duration": 5.0}},
			want:     metering.SubtypeExtend,
		},
		{
			name:     "audio tts endpoint",
			endpoint: "fal-ai/kokoro/text-to-speech",
			op:       metering.OperationAudio,
			result:   map[string]interface{}{"audio": map[string]interface{}{"url": "https://x/a.mp3", "duration": 2.0}},
			want:     metering.SubtypeTTS,
		},
		{
			name:     "audio music endpoint sends synthesis",
			endpoint: "fal-ai/minimax-music",
			op:       metering.OperationAudio,
			result:   map[string]interface{}{"audio": map[string]interface{}{"url": "https://x/a.mp3", "duration": 30.0}},
			want:     metering.SubtypeSynthesis,
		},
		{
			name:     "audio transcription endpoint",
			endpoint: "fal-ai/whisper",
			op:       metering.OperationAudio,
			result:   map[string]interface{}{"text": "hello"},
			want:     metering.SubtypeTranscription,
		},
		{
			name:     "caller metadata overrides the detected subtype",
			endpoint: "fal-ai/flux/schnell",
			op:       metering.OperationImage,
			result:   map[string]interface{}{"images": []interface{}{}},
			metadata: map[string]interface{}{"operationSubtype": metering.SubtypeUpscale},
			want:     metering.SubtypeUpscale,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payload := buildPayloadFromResult(tt.endpoint, tt.op, tt.result, nil, tt.metadata, time.Second, time.Now(), false, "")
			require.NotNil(t, payload)
			assert.Equal(t, tt.want, payload.OperationSubtype)
			assert.NotContains(t, payload.Attributes, "operationSubtype")
		})
	}
}
