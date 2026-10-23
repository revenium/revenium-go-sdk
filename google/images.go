package google

import (
	"context"
	"time"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/revenium/revenium-go-sdk/core/metering"
	"google.golang.org/genai"
)

type imageCall struct {
	model       string
	subtype     string
	metadata    map[string]interface{}
	requestTime time.Time
	sending     *sentRequest
}

type imageResult struct {
	actual     int
	requested  int
	resolution string
}

func newImageCall(ctx context.Context, model, subtype string) (context.Context, imageCall) {
	ctx, sending := watchSending(ctx)
	return ctx, imageCall{model: model, subtype: subtype, metadata: core.GetUsageMetadata(ctx), requestTime: time.Now(), sending: sending}
}

func (m *ModelsInterface) GenerateImage(
	ctx context.Context,
	model string,
	prompt string,
	config *genai.GenerateImagesConfig,
) (*genai.GenerateImagesResponse, error) {
	core.Debug("GenerateImage called with model: %s", model)

	if err := m.checkCostControls(ctx, model); err != nil {
		core.Debug("GenerateImage blocked by enforcement: %v", err)
		return nil, err
	}

	ctx, call := newImageCall(ctx, model, metering.SubtypeGeneration)

	resp, err := m.client.Models.GenerateImages(ctx, model, prompt, config)
	if err != nil {
		core.Debug("GenerateImage error: %v", err)
		m.recordImageFailure(call, err)
		return nil, err
	}

	result := imageResult{actual: len(resp.GeneratedImages), requested: len(resp.GeneratedImages)}
	if config != nil {
		result.requested = requestedImages(config.NumberOfImages, result.actual)
		result.resolution = mapAspectRatioToResolution(config.AspectRatio)
	}
	m.recordImages(call, result)
	return resp, nil
}

func (m *ModelsInterface) EditImage(
	ctx context.Context,
	model string,
	prompt string,
	referenceImages []genai.ReferenceImage,
	config *genai.EditImageConfig,
) (*genai.EditImageResponse, error) {
	core.Debug("EditImage called with model: %s", model)

	if err := m.checkCostControls(ctx, model); err != nil {
		core.Debug("EditImage blocked by enforcement: %v", err)
		return nil, err
	}

	ctx, call := newImageCall(ctx, model, metering.SubtypeEdit)

	resp, err := m.client.Models.EditImage(ctx, model, prompt, referenceImages, config)
	if err != nil {
		core.Debug("EditImage error: %v", err)
		m.recordImageFailure(call, err)
		return nil, err
	}

	result := imageResult{actual: len(resp.GeneratedImages), requested: len(resp.GeneratedImages)}
	if config != nil {
		result.requested = requestedImages(config.NumberOfImages, result.actual)
		result.resolution = mapAspectRatioToResolution(config.AspectRatio)
	}
	m.recordImages(call, result)
	return resp, nil
}

func (m *ModelsInterface) UpscaleImage(
	ctx context.Context,
	model string,
	image *genai.Image,
	upscaleFactor string,
	config *genai.UpscaleImageConfig,
) (*genai.UpscaleImageResponse, error) {
	core.Debug("UpscaleImage called with model: %s", model)

	if err := m.checkCostControls(ctx, model); err != nil {
		core.Debug("UpscaleImage blocked by enforcement: %v", err)
		return nil, err
	}

	ctx, call := newImageCall(ctx, model, metering.SubtypeUpscale)

	resp, err := m.client.Models.UpscaleImage(ctx, model, image, upscaleFactor, config)
	if err != nil {
		core.Debug("UpscaleImage error: %v", err)
		m.recordImageFailure(call, err)
		return nil, err
	}

	result := imageResult{actual: len(resp.GeneratedImages), requested: 1}
	if upscaleFactor != "" {
		result.resolution = mapUpscaleFactorToResolution(upscaleFactor)
	}
	m.recordImages(call, result)
	return resp, nil
}

func requestedImages(numberOfImages int32, actual int) int {
	if numberOfImages > 0 {
		return int(numberOfImages)
	}
	return actual
}

func (m *ModelsInterface) imagePayload(call imageCall) *metering.PayloadBuilder {
	provider, model := m.provider.mediaIdentity(metering.OperationImage, call.model)
	return metering.NewPayload(metering.OperationImage, model, provider).
		WithTiming(call.requestTime, time.Since(call.requestTime)).
		WithOperationSubtype(call.subtype)
}

func (m *ModelsInterface) recordImageFailure(call imageCall, err error) {
	if !call.sending.left() {
		core.Debug("Image %s refused before any request reached Google; not recorded: %v", call.subtype, err)
		return
	}
	payload := m.imagePayload(call).Build()
	metering.ApplyFailedCall(payload, callFailure(err), call.metadata)
	m.parent.metering.Send(payload)
}

func (m *ModelsInterface) recordImages(call imageCall, result imageResult) {
	core.Debug("Image %s completed, images: %d", call.subtype, result.actual)
	payload := m.imagePayload(call).
		WithImageBilling(result.actual, result.requested).
		WithBillingUnit(metering.BillingPerImage).
		WithResolution(result.resolution).
		Build()
	m.send(payload, call.metadata)
}

func (m *ModelsInterface) send(payload *metering.MeteringPayload, metadata map[string]interface{}) {
	metering.ApplyMetadata(payload, metadata)
	m.parent.metering.Send(payload)
}
