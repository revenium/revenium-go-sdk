package fal

import (
	"net/url"
	"regexp"
	"strings"

	"github.com/revenium/revenium-go-sdk/core/metering"
)

var (
	imagePatterns = regexp.MustCompile(`(?i)flux|stable-diffusion|recraft|bria|imagen|nano-banana|sdxl|dreambooth|photomaker|face-swap|upscale/image|background/remove|nsfw|illusion|controlnet|ip-adapter|inpaint|img2img|omnigen|cat-vton|try-on|kolors|aura-flow|playground`)
	videoPatterns = regexp.MustCompile(`(?i)video|motion|animate|runway|luma|kling|veo|sora|ltx|minimax-video|cogvideo|hunyuan|\bwan-|mochi|haiper`)
	audioPatterns = regexp.MustCompile(`(?i)audio|speech|voice|tts|whisper|chatterbox|lava-sr|sfx|sound|music|f5-tts|\bdia\b|kokoro|mars6|parler`)
	chatPatterns  = regexp.MustCompile(`(?i)openrouter|llm|text-generation`)
)

// DetectFromEndpointID infers the media operation type from a fal endpoint identifier
func DetectFromEndpointID(endpointID string) metering.OperationType {
	switch {
	case audioPatterns.MatchString(endpointID):
		return metering.OperationAudio
	case chatPatterns.MatchString(endpointID):
		return metering.OperationChat
	case videoPatterns.MatchString(endpointID):
		return metering.OperationVideo
	case imagePatterns.MatchString(endpointID):
		return metering.OperationImage
	}
	return metering.OperationImage
}

// CorrectFromResponse refines the inferred operation type using fields present in the response payload
func CorrectFromResponse(initial metering.OperationType, response map[string]interface{}) metering.OperationType {
	if len(response) == 0 {
		return initial
	}

	fileURLPath := ""
	if raw, ok := response["file_url"].(string); ok {
		fileURLPath = strings.ToLower(extractPath(raw))
	}

	if response["video"] != nil || strings.HasSuffix(fileURLPath, ".mp4") {
		return metering.OperationVideo
	}
	if response["audio_url"] != nil || response["audio"] != nil ||
		strings.HasSuffix(fileURLPath, ".mp3") || strings.HasSuffix(fileURLPath, ".wav") {
		return metering.OperationAudio
	}
	if _, ok := response["images"]; ok {
		return metering.OperationImage
	}
	if usage, ok := response["usage"].(map[string]interface{}); ok && len(usage) > 0 {
		return metering.OperationChat
	}

	return initial
}

// DetectMediaType returns the operation type for an endpoint, refining the guess with response data when available
func DetectMediaType(endpointID string, response map[string]interface{}) metering.OperationType {
	estimated := DetectFromEndpointID(endpointID)
	if response == nil {
		return estimated
	}
	return CorrectFromResponse(estimated, response)
}

func extractPath(raw string) string {
	if u, err := url.Parse(raw); err == nil && u.Path != "" {
		return u.Path
	}
	if idx := strings.Index(raw, "?"); idx >= 0 {
		return raw[:idx]
	}
	return raw
}
