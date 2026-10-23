package openai

import (
	"context"
	"time"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/packages/param"
	"github.com/revenium/revenium-go-sdk/core"
	"github.com/revenium/revenium-go-sdk/core/enforcement"
	"github.com/revenium/revenium-go-sdk/core/metering"
)

type ImagesInterface struct {
	client   openai.Client
	config   *Config
	provider Provider
	parent   *ReveniumOpenAI
}

type imageCall struct {
	model       string
	subtype     string
	requested   int
	details     metering.ImageDetails
	metadata    map[string]interface{}
	requestTime time.Time
}

func newImageCall(ctx context.Context, model, subtype string, n param.Opt[int64], details metering.ImageDetails) imageCall {
	requested := 1
	if n.Valid() {
		requested = int(n.Value)
	}
	return imageCall{
		model:       model,
		subtype:     subtype,
		requested:   requested,
		details:     details,
		metadata:    core.GetUsageMetadata(ctx),
		requestTime: time.Now(),
	}
}

func (i *ImagesInterface) Generate(ctx context.Context, params openai.ImageGenerateParams) (*openai.ImagesResponse, error) {
	model := i.recordedModel(params.Model, generationDefaultModel(params))
	if err := i.checkEnforcement(ctx, model, "Images.Generate"); err != nil {
		return nil, err
	}
	call := newImageCall(ctx, i.config.mediaModel(i.provider, model), metering.SubtypeGeneration, params.N, metering.ImageDetails{
		Resolution: string(params.Size),
		Quality:    string(params.Quality),
		Style:      string(params.Style),
		Format:     string(params.ResponseFormat),
	})
	resp, err := i.client.Images.Generate(ctx, params)
	return i.record(call, resp, err)
}

func (i *ImagesInterface) Edit(ctx context.Context, params openai.ImageEditParams) (*openai.ImagesResponse, error) {
	model := i.recordedModel(params.Model, editDefaultModel)
	if err := i.checkEnforcement(ctx, model, "Images.Edit"); err != nil {
		return nil, err
	}
	call := newImageCall(ctx, i.config.mediaModel(i.provider, model), metering.SubtypeEdit, params.N, metering.ImageDetails{
		Resolution: string(params.Size),
		Quality:    string(params.Quality),
		Format:     string(params.ResponseFormat),
	})
	resp, err := i.client.Images.Edit(ctx, params)
	return i.record(call, resp, err)
}

func (i *ImagesInterface) CreateVariation(ctx context.Context, params openai.ImageNewVariationParams) (*openai.ImagesResponse, error) {
	model := i.recordedModel(params.Model, variationDefaultModel)
	if err := i.checkEnforcement(ctx, model, "Images.CreateVariation"); err != nil {
		return nil, err
	}
	call := newImageCall(ctx, i.config.mediaModel(i.provider, model), metering.SubtypeVariation, params.N, metering.ImageDetails{
		Resolution: string(params.Size),
		Format:     string(params.ResponseFormat),
	})
	resp, err := i.client.Images.NewVariation(ctx, params)
	return i.record(call, resp, err)
}

const (
	editDefaultModel      = openai.ImageModelGPTImage1_5
	variationDefaultModel = openai.ImageModelDallE2
)

func generationDefaultModel(params openai.ImageGenerateParams) openai.ImageModel {
	if usesGPTImageOnlyOptions(params) {
		return openai.ImageModelGPTImage1
	}
	return openai.ImageModelDallE2
}

func usesGPTImageOnlyOptions(params openai.ImageGenerateParams) bool {
	return params.Background != "" || params.Moderation != "" || params.OutputFormat != "" ||
		params.OutputCompression.Valid() || params.PartialImages.Valid() ||
		isGPTImageQuality(params.Quality) || isGPTImageSize(params.Size)
}

func isGPTImageQuality(quality openai.ImageGenerateParamsQuality) bool {
	switch quality {
	case openai.ImageGenerateParamsQualityLow, openai.ImageGenerateParamsQualityMedium, openai.ImageGenerateParamsQualityHigh,
		openai.ImageGenerateParamsQualityXhigh, openai.ImageGenerateParamsQualityMax:
		return true
	default:
		return false
	}
}

func isGPTImageSize(size openai.ImageGenerateParamsSize) bool {
	switch size {
	case openai.ImageGenerateParamsSizeAuto, openai.ImageGenerateParamsSize1536x1024, openai.ImageGenerateParamsSize1024x1536:
		return true
	default:
		return false
	}
}

// recordedModel leaves the model empty off api.openai.com because Azure, xAI and
// OpenAI-compatible proxies choose their own default, which the reply does not name.
func (i *ImagesInterface) recordedModel(model, openAIDefault openai.ImageModel) string {
	if model != "" || i.provider != ProviderOpenAI || !i.config.callsOpenAIDirectly() {
		return string(model)
	}
	return string(openAIDefault)
}

func (i *ImagesInterface) checkEnforcement(ctx context.Context, model, operation string) error {
	err := i.parent.enforcer.Check(enforcement.NewEvalContext(ctx, model, i.provider.String()))
	if err != nil {
		core.Debug("%s blocked by enforcement: %v", operation, err)
	}
	return err
}

func (i *ImagesInterface) record(call imageCall, resp *openai.ImagesResponse, err error) (*openai.ImagesResponse, error) {
	builder := i.provider.mediaRecord(metering.OperationImage, call.model).
		WithTiming(call.requestTime, time.Since(call.requestTime)).
		WithOperationSubtype(call.subtype)
	if err != nil {
		i.parent.metering.Send(failedCallRecord(builder, err, call.metadata))
		return nil, err
	}
	payload := builder.
		WithImageBilling(len(resp.Data), call.requested).
		WithBillingUnit(metering.BillingPerImage).
		WithImageDetails(renderedDetails(call.details, resp)).
		WithTokens(resp.Usage.InputTokens, resp.Usage.OutputTokens, resp.Usage.TotalTokens).
		Build()
	metering.ApplyMetadata(payload, call.metadata)
	i.parent.metering.Send(payload)
	return resp, nil
}

// renderedDetails prefers the size and quality the response reports, because gpt-image
// models resolve "auto" to the concrete values they render and bill.
func renderedDetails(requested metering.ImageDetails, resp *openai.ImagesResponse) metering.ImageDetails {
	if size := string(resp.Size); size != "" {
		requested.Resolution = size
	}
	if quality := string(resp.Quality); quality != "" {
		requested.Quality = quality
	}
	return requested
}
