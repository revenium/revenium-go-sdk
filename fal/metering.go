package fal

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/revenium/revenium-go-sdk/core/metering"
)

const (
	meteredModelPrefix = "fal_ai/"
	falOwnNamespace    = "fal-ai/"
)

// normalizeModelName names a fal endpoint the way the price catalog does: "fal_ai/" followed by
// the endpoint id, whose first segment is the publishing namespace ("fal-ai", or a partner such as
// "bria"). Only an id with no namespace at all is malformed.
func normalizeModelName(model string) string {
	endpointID := strings.TrimPrefix(model, meteredModelPrefix)
	if !strings.Contains(strings.Trim(endpointID, "/"), "/") {
		core.Warn("Model name '%s' has no namespace segment. Recording it as '%s%s%s'",
			model, meteredModelPrefix, falOwnNamespace, endpointID)
		return meteredModelPrefix + falOwnNamespace + endpointID
	}
	return meteredModelPrefix + endpointID
}

func buildImageMeteringPayload(
	model string,
	imageResp *FalImageResponse,
	metadata map[string]interface{},
	duration time.Duration,
	requestTime time.Time,
	capturePrompts bool,
	prompt string,
) *metering.MeteringPayload {
	b := metering.NewPayload(metering.OperationImage, normalizeModelName(model), "fal_ai").
		WithTiming(requestTime, duration).
		WithModelSource("FAL").
		WithOperationSubtype(detectImageSubtype(model))

	images := imageResp.OutputImages()
	if imageResp != nil {
		b.WithImageBilling(len(images), len(images))
	}
	if len(images) > 0 {
		b.WithAttributes(map[string]interface{}{
			"width":  images[0].Width,
			"height": images[0].Height,
		})
	}

	payload := b.Build()
	metering.ApplyMetadata(payload, metadata)

	if capturePrompts {
		output := ""
		if outputURLs := imageLinks(images); len(outputURLs) > 0 {
			if outputJSON, err := json.Marshal(outputURLs); err == nil {
				output = string(outputJSON)
			}
		}
		metering.ApplyCapturedPrompt(payload, prompt, output)
	}

	return payload
}

func buildVideoMeteringPayload(
	model string,
	videoResp *FalVideoResponse,
	metadata map[string]interface{},
	duration time.Duration,
	requestTime time.Time,
	requestedSeconds float64,
	capturePrompts bool,
	prompt string,
	outputURL string,
) *metering.MeteringPayload {
	b := metering.NewPayload(metering.OperationVideo, normalizeModelName(model), "fal_ai").
		WithTiming(requestTime, duration).
		WithModelSource("FAL").
		WithOperationSubtype(detectVideoSubtype(model))

	actualDur := requestedSeconds
	requestedDur := requestedSeconds

	if videoResp != nil && videoResp.Video.Seconds() > 0 {
		actualDur = videoResp.Video.Seconds()
	}
	if requestedDur == 0 {
		requestedDur = actualDur
	}
	b.WithVideoDuration(actualDur, requestedDur)
	if actualDur == 0 {
		reason := unknownVideoLengthReason(model)
		core.Warn("%s. Set duration on the request to bill the real length", reason)
		b.WithError(reason)
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
		metering.ApplyCapturedPrompt(payload, prompt, outputURL)
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
		WithModelSource("FAL").
		WithOperationSubtype(detectAudioSubtype(model))

	if seconds := audioResp.DurationSeconds(); seconds > 0 {
		b.WithAudioDuration(seconds)
	}

	payload := b.Build()
	metering.ApplyMetadata(payload, metadata)

	if capturePrompts {
		metering.ApplyCapturedPrompt(payload, prompt, outputURL)
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
		return buildVideoMeteringPayload(model, videoFromMap(result), metadata, duration, requestTime, requestedVideoSeconds(model, input), capturePrompts, prompt, capturableLink(videoURLFromResult(result)))
	case metering.OperationAudio:
		return buildAudioMeteringPayload(model, audioFromMap(result), metadata, duration, requestTime, capturePrompts, prompt, capturableLink(audioURLFromResult(result)))
	case metering.OperationChat:
		return buildChatMeteringPayload(model, result, metadata, duration, requestTime, capturePrompts, prompt)
	default:
		return buildImageMeteringPayload(model, imageFromMap(result), metadata, duration, requestTime, capturePrompts, prompt)
	}
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

func imageLinks(images []FalImage) []string {
	var urls []string
	for _, img := range images {
		if link := capturableLink(img.URL); link != "" {
			urls = append(urls, link)
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
