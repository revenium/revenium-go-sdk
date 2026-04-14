package fal

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/revenium/revenium-go-sdk/core/metering"
)

func normalizeModelName(model string) string {
	const litellmPrefix = "fal_ai/"
	const falEndpointPrefix = "fal-ai/"

	if strings.HasPrefix(model, litellmPrefix+falEndpointPrefix) {
		return model
	}

	if strings.HasPrefix(model, litellmPrefix) {
		remainder := strings.TrimPrefix(model, litellmPrefix)
		core.Warn("Model name '%s' has 'fal_ai/' prefix but missing 'fal-ai/' segment. Auto-normalizing.", model)
		return litellmPrefix + falEndpointPrefix + remainder
	}

	if strings.HasPrefix(model, falEndpointPrefix) {
		return litellmPrefix + model
	}

	core.Warn("Model name '%s' is missing 'fal-ai/' prefix. Auto-normalizing to '%s%s%s'",
		model, litellmPrefix, falEndpointPrefix, model)
	return litellmPrefix + falEndpointPrefix + model
}

func buildImageMeteringPayload(
	model string,
	imageResp *FalImageResponse,
	metadata map[string]interface{},
	duration time.Duration,
	requestTime time.Time,
	capturePrompts bool,
	prompt string,
	outputURLs []string,
) *metering.MeteringPayload {
	b := metering.NewPayload(metering.OperationImage, normalizeModelName(model), "fal_ai").
		WithTiming(requestTime, duration).
		WithModelSource("FAL")

	if imageResp != nil {
		imageCount := len(imageResp.Images)
		b.WithImageBilling(imageCount, imageCount)

		if len(imageResp.Images) > 0 {
			b.WithAttributes(map[string]interface{}{
				"width":  imageResp.Images[0].Width,
				"height": imageResp.Images[0].Height,
			})
		}
	}

	payload := b.Build()
	metering.ApplyMetadata(payload, metadata)

	if capturePrompts {
		output := ""
		if len(outputURLs) > 0 {
			if outputJSON, err := json.Marshal(outputURLs); err == nil {
				output = string(outputJSON)
			}
		}
		applyCapturedPrompt(payload, prompt, output)
	}

	return payload
}

func buildVideoMeteringPayload(
	model string,
	videoResp *FalVideoResponse,
	metadata map[string]interface{},
	duration time.Duration,
	requestTime time.Time,
	requestedDuration string,
	capturePrompts bool,
	prompt string,
	outputURL string,
) *metering.MeteringPayload {
	b := metering.NewPayload(metering.OperationVideo, normalizeModelName(model), "fal_ai").
		WithTiming(requestTime, duration).
		WithModelSource("FAL")

	var reqDurSeconds float64
	if requestedDuration != "" {
		trimmed := strings.TrimSpace(requestedDuration)
		if parsed, err := strconv.ParseFloat(trimmed, 64); err == nil {
			reqDurSeconds = parsed
		} else {
			core.Warn("Failed to parse requestedDuration '%s': %v - video billing may fail with 422", requestedDuration, err)
		}
	}

	actualDur := reqDurSeconds
	requestedDur := reqDurSeconds

	if videoResp != nil && videoResp.Video.Duration > 0 {
		actualDur = videoResp.Video.Duration
	}

	if actualDur > 0 || requestedDur > 0 {
		if actualDur == 0 {
			actualDur = requestedDur
		}
		if requestedDur == 0 {
			requestedDur = actualDur
		}
		b.WithVideoDuration(actualDur, requestedDur)
	}

	if videoResp != nil {
		attrs := make(map[string]interface{})
		if videoResp.Video.Width > 0 {
			attrs["width"] = videoResp.Video.Width
		}
		if videoResp.Video.Height > 0 {
			attrs["height"] = videoResp.Video.Height
		}
		if len(attrs) > 0 {
			b.WithAttributes(attrs)
		}
	}

	payload := b.Build()
	metering.ApplyMetadata(payload, metadata)

	if capturePrompts {
		applyCapturedPrompt(payload, prompt, outputURL)
	}

	return payload
}

func buildAudioMeteringPayload(
	model string,
	audioResp *FalAudioResponse,
	metadata map[string]interface{},
	duration time.Duration,
	requestTime time.Time,
	capturePrompts bool,
	prompt string,
	outputURL string,
) *metering.MeteringPayload {
	b := metering.NewPayload(metering.OperationAudio, normalizeModelName(model), "fal_ai").
		WithTiming(requestTime, duration).
		WithModelSource("FAL")

	if audioResp != nil && audioResp.Audio.Duration > 0 {
		b.WithAudioDuration(audioResp.Audio.Duration)
	}

	payload := b.Build()
	metering.ApplyMetadata(payload, metadata)

	if capturePrompts {
		applyCapturedPrompt(payload, prompt, outputURL)
	}

	return payload
}

func buildPayloadFromResult(
	model string,
	op metering.OperationType,
	result map[string]interface{},
	input map[string]interface{},
	metadata map[string]interface{},
	duration time.Duration,
	requestTime time.Time,
	capturePrompts bool,
	prompt string,
) *metering.MeteringPayload {
	switch op {
	case metering.OperationVideo:
		return buildVideoMeteringPayload(model, videoFromMap(result), metadata, duration, requestTime, requestedDurationFromInput(input), capturePrompts, prompt, videoURLFromResult(result))
	case metering.OperationAudio:
		return buildAudioMeteringPayload(model, audioFromMap(result), metadata, duration, requestTime, capturePrompts, prompt, audioURLFromResult(result))
	case metering.OperationChat:
		return buildChatMeteringPayload(model, result, metadata, duration, requestTime, capturePrompts, prompt)
	default:
		return buildImageMeteringPayload(model, imageFromMap(result), metadata, duration, requestTime, capturePrompts, prompt, imageURLsFromResult(result))
	}
}

func requestedDurationFromInput(input map[string]interface{}) string {
	if input == nil {
		return ""
	}
	switch v := input["duration"].(type) {
	case string:
		return v
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case int:
		return strconv.Itoa(v)
	}
	return ""
}

func imageFromMap(result map[string]interface{}) *FalImageResponse {
	if result == nil {
		return nil
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return nil
	}
	var resp FalImageResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil
	}
	return &resp
}

func videoFromMap(result map[string]interface{}) *FalVideoResponse {
	if result == nil {
		return nil
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return nil
	}
	var resp FalVideoResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil
	}
	return &resp
}

func audioFromMap(result map[string]interface{}) *FalAudioResponse {
	if result == nil {
		return nil
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return nil
	}
	var resp FalAudioResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil
	}
	return &resp
}

func imageURLsFromResult(result map[string]interface{}) []string {
	if result == nil {
		return nil
	}
	imgs, ok := result["images"].([]interface{})
	if !ok {
		return nil
	}
	var urls []string
	for _, img := range imgs {
		m, ok := img.(map[string]interface{})
		if !ok {
			continue
		}
		if u, ok := m["url"].(string); ok && u != "" {
			urls = append(urls, u)
		}
	}
	return urls
}

func videoURLFromResult(result map[string]interface{}) string {
	if result == nil {
		return ""
	}
	if v, ok := result["video"].(map[string]interface{}); ok {
		if u, ok := v["url"].(string); ok {
			return u
		}
	}
	if u, ok := result["video_url"].(string); ok {
		return u
	}
	return ""
}

func audioURLFromResult(result map[string]interface{}) string {
	if result == nil {
		return ""
	}
	if v, ok := result["audio"].(map[string]interface{}); ok {
		if u, ok := v["url"].(string); ok {
			return u
		}
	}
	if u, ok := result["audio_url"].(string); ok {
		return u
	}
	return ""
}
