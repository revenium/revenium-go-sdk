package fal

import (
	"testing"

	"github.com/revenium/revenium-go-sdk/core/metering"
	"github.com/stretchr/testify/assert"
)

func TestDetectFromEndpointID(t *testing.T) {
	cases := map[string]metering.OperationType{
		"fal-ai/flux/dev":                              metering.OperationImage,
		"fal-ai/stable-diffusion-v3":                   metering.OperationImage,
		"fal-ai/recraft-v3":                            metering.OperationImage,
		"fal-ai/imagen-3":                              metering.OperationImage,
		"fal-ai/kling-video/v1/standard/text-to-video": metering.OperationVideo,
		"fal-ai/veo-3":                                 metering.OperationVideo,
		"fal-ai/sora":                                  metering.OperationVideo,
		"fal-ai/luma-dream-machine":                    metering.OperationVideo,
		"fal-ai/whisper":                               metering.OperationAudio,
		"fal-ai/kokoro/text-to-speech":                 metering.OperationAudio,
		"fal-ai/f5-tts":                                metering.OperationAudio,
		"fal-ai/openrouter/llama-3":                    metering.OperationChat,
		"fal-ai/llm-gateway":                           metering.OperationChat,
		"fal-ai/totally-unknown":                       metering.OperationImage,
		"fal-ai/wan-2.1-text-to-video":                 metering.OperationVideo,
		"fal-ai/dia":                                   metering.OperationAudio,
	}

	for endpoint, want := range cases {
		t.Run(endpoint, func(t *testing.T) {
			assert.Equal(t, want, DetectFromEndpointID(endpoint))
		})
	}
}

func TestCorrectFromResponse(t *testing.T) {
	tests := []struct {
		name     string
		initial  metering.OperationType
		response map[string]interface{}
		want     metering.OperationType
	}{
		{"video shape", metering.OperationImage, map[string]interface{}{"video": map[string]interface{}{"url": "x"}}, metering.OperationVideo},
		{"video file_url mp4", metering.OperationImage, map[string]interface{}{"file_url": "https://x.fal/output.mp4"}, metering.OperationVideo},
		{"audio shape", metering.OperationImage, map[string]interface{}{"audio": map[string]interface{}{"url": "x"}}, metering.OperationAudio},
		{"audio file_url mp3", metering.OperationVideo, map[string]interface{}{"file_url": "https://x.fal/x.mp3"}, metering.OperationAudio},
		{"image shape", metering.OperationVideo, map[string]interface{}{"images": []interface{}{map[string]interface{}{"url": "x"}}}, metering.OperationImage},
		{"chat usage shape", metering.OperationImage, map[string]interface{}{"usage": map[string]interface{}{"total_tokens": 12}}, metering.OperationChat},
		{"empty falls back", metering.OperationVideo, map[string]interface{}{}, metering.OperationVideo},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, CorrectFromResponse(tt.initial, tt.response))
		})
	}
}

func TestDetectFromEndpointID_NoFalsePositiveOnGenericWords(t *testing.T) {
	cases := map[string]metering.OperationType{
		"fal-ai/imagination-engine":  metering.OperationImage,
		"fal-ai/homepage-generator":  metering.OperationImage,
		"fal-ai/something-with-chat": metering.OperationImage,
		"fal-ai/random-endpoint":     metering.OperationImage,
		"fal-ai/taiwan-localizer":    metering.OperationImage,
		"fal-ai/swan-detector":       metering.OperationImage,
		"fal-ai/media-converter":     metering.OperationImage,
		"fal-ai/india-translator":    metering.OperationImage,
	}
	for endpoint, want := range cases {
		t.Run(endpoint, func(t *testing.T) {
			assert.Equal(t, want, DetectFromEndpointID(endpoint))
		})
	}
}

func TestDetectMediaType_PrefersResponseShape(t *testing.T) {
	op := DetectMediaType("fal-ai/flux/dev", map[string]interface{}{"video": map[string]interface{}{"url": "v"}})
	assert.Equal(t, metering.OperationVideo, op)
}
