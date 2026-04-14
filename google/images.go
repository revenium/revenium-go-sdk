package google

import (
	"context"
	"time"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/revenium/revenium-go-sdk/core/metering"
	"google.golang.org/genai"
)

func (m *ModelsInterface) GenerateImage(
	ctx context.Context,
	model string,
	prompt string,
	config *genai.GenerateImagesConfig,
) (*genai.GenerateImagesResponse, error) {
	metadata := core.GetUsageMetadata(ctx)

	core.Debug("GenerateImage called with model: %s", model)

	requestTime := time.Now()

	resp, err := m.client.Models.GenerateImages(ctx, model, prompt, config)

	duration := time.Since(requestTime)

	if err != nil {
		core.Debug("GenerateImage error: %v", err)
		payload := metering.NewPayload(metering.OperationImage, model, m.provider.String()).
			WithTiming(requestTime, duration).
			WithError(err.Error()).
			Build()
		metering.ApplyMetadata(payload, metadata)
		m.parent.metering.Send(payload)
		return nil, err
	}

	actual := len(resp.GeneratedImages)
	requested := actual
	attrs := map[string]interface{}{
		"operationSubtype": "generation",
	}
	if config != nil {
		if config.NumberOfImages > 0 {
			requested = int(config.NumberOfImages)
		}
		if config.AspectRatio != "" {
			attrs["resolution"] = mapAspectRatioToResolution(config.AspectRatio)
		}
	}

	core.Debug("GenerateImage completed in %v, images: %d", duration, actual)

	payload := metering.NewPayload(metering.OperationImage, model, m.provider.String()).
		WithTiming(requestTime, duration).
		WithImageBilling(actual, requested).
		WithAttributes(attrs).
		Build()
	metering.ApplyMetadata(payload, metadata)
	m.parent.metering.Send(payload)

	return resp, nil
}

func (m *ModelsInterface) EditImage(
	ctx context.Context,
	model string,
	prompt string,
	referenceImages []genai.ReferenceImage,
	config *genai.EditImageConfig,
) (*genai.EditImageResponse, error) {
	metadata := core.GetUsageMetadata(ctx)

	core.Debug("EditImage called with model: %s", model)

	requestTime := time.Now()

	resp, err := m.client.Models.EditImage(ctx, model, prompt, referenceImages, config)

	duration := time.Since(requestTime)

	if err != nil {
		core.Debug("EditImage error: %v", err)
		payload := metering.NewPayload(metering.OperationImage, model, m.provider.String()).
			WithTiming(requestTime, duration).
			WithError(err.Error()).
			Build()
		metering.ApplyMetadata(payload, metadata)
		m.parent.metering.Send(payload)
		return nil, err
	}

	actual := len(resp.GeneratedImages)
	requested := actual
	attrs := map[string]interface{}{
		"operationSubtype": "edit",
	}
	if config != nil && config.NumberOfImages > 0 {
		requested = int(config.NumberOfImages)
	}

	core.Debug("EditImage completed in %v, images: %d", duration, actual)

	payload := metering.NewPayload(metering.OperationImage, model, m.provider.String()).
		WithTiming(requestTime, duration).
		WithImageBilling(actual, requested).
		WithAttributes(attrs).
		Build()
	metering.ApplyMetadata(payload, metadata)
	m.parent.metering.Send(payload)

	return resp, nil
}

func (m *ModelsInterface) UpscaleImage(
	ctx context.Context,
	model string,
	image *genai.Image,
	upscaleFactor string,
	config *genai.UpscaleImageConfig,
) (*genai.UpscaleImageResponse, error) {
	metadata := core.GetUsageMetadata(ctx)

	core.Debug("UpscaleImage called with model: %s", model)

	requestTime := time.Now()

	resp, err := m.client.Models.UpscaleImage(ctx, model, image, upscaleFactor, config)

	duration := time.Since(requestTime)

	if err != nil {
		core.Debug("UpscaleImage error: %v", err)
		payload := metering.NewPayload(metering.OperationImage, model, m.provider.String()).
			WithTiming(requestTime, duration).
			WithError(err.Error()).
			Build()
		metering.ApplyMetadata(payload, metadata)
		m.parent.metering.Send(payload)
		return nil, err
	}

	actual := len(resp.GeneratedImages)
	attrs := map[string]interface{}{
		"operationSubtype": "upscale",
		"upscaleFactor":    upscaleFactor,
	}

	core.Debug("UpscaleImage completed in %v, images: %d", duration, actual)

	payload := metering.NewPayload(metering.OperationImage, model, m.provider.String()).
		WithTiming(requestTime, duration).
		WithImageBilling(actual, 1).
		WithAttributes(attrs).
		Build()
	metering.ApplyMetadata(payload, metadata)
	m.parent.metering.Send(payload)

	return resp, nil
}
