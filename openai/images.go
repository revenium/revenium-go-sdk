package openai

import (
	"context"
	"time"

	"github.com/openai/openai-go/v3"
	"github.com/revenium/revenium-go-sdk/core"
	"github.com/revenium/revenium-go-sdk/core/metering"
)

type ImagesInterface struct {
	client   openai.Client
	config   *Config
	provider Provider
	parent   *ReveniumOpenAI
}

func (i *ImagesInterface) Generate(ctx context.Context, params openai.ImageGenerateParams) (*openai.ImagesResponse, error) {
	metadata := core.GetUsageMetadata(ctx)
	model := string(params.Model)
	providerStr := i.provider.String()
	requestTime := time.Now()

	requested := 1
	if params.N.Valid() {
		requested = int(params.N.Value)
	}

	resp, err := i.client.Images.Generate(ctx, params)
	if err != nil {
		duration := time.Since(requestTime)
		payload := i.buildErrorPayload(model, metadata, duration, providerStr, requestTime, err.Error())
		i.parent.metering.Send(payload)
		return nil, err
	}

	duration := time.Since(requestTime)
	actual := len(resp.Data)

	attrs := map[string]interface{}{
		"billing_unit":     "per_image",
		"operationSubtype": "generation",
	}
	if s := string(params.Size); s != "" {
		attrs["resolution"] = s
	}
	if q := string(params.Quality); q != "" {
		attrs["quality"] = q
	}
	if st := string(params.Style); st != "" {
		attrs["style"] = st
	}
	if rf := string(params.ResponseFormat); rf != "" {
		attrs["response_format"] = rf
	}

	payload := metering.NewPayload(metering.OperationImage, model, providerStr).
		WithTiming(requestTime, duration).
		WithImageBilling(actual, requested).
		WithAttributes(attrs).
		Build()

	metering.ApplyMetadata(payload, metadata)
	i.parent.metering.Send(payload)

	return resp, nil
}

func (i *ImagesInterface) Edit(ctx context.Context, params openai.ImageEditParams) (*openai.ImagesResponse, error) {
	metadata := core.GetUsageMetadata(ctx)
	model := string(params.Model)
	providerStr := i.provider.String()
	requestTime := time.Now()

	requested := 1
	if params.N.Valid() {
		requested = int(params.N.Value)
	}

	resp, err := i.client.Images.Edit(ctx, params)
	if err != nil {
		duration := time.Since(requestTime)
		payload := i.buildErrorPayload(model, metadata, duration, providerStr, requestTime, err.Error())
		i.parent.metering.Send(payload)
		return nil, err
	}

	duration := time.Since(requestTime)
	actual := len(resp.Data)

	attrs := map[string]interface{}{
		"billing_unit":     "per_image",
		"operationSubtype": "edit",
		"has_mask":         params.Mask != nil,
	}
	if s := string(params.Size); s != "" {
		attrs["resolution"] = s
	}
	if rf := string(params.ResponseFormat); rf != "" {
		attrs["response_format"] = rf
	}

	payload := metering.NewPayload(metering.OperationImage, model, providerStr).
		WithTiming(requestTime, duration).
		WithImageBilling(actual, requested).
		WithAttributes(attrs).
		Build()

	metering.ApplyMetadata(payload, metadata)
	i.parent.metering.Send(payload)

	return resp, nil
}

func (i *ImagesInterface) CreateVariation(ctx context.Context, params openai.ImageNewVariationParams) (*openai.ImagesResponse, error) {
	metadata := core.GetUsageMetadata(ctx)
	model := string(params.Model)
	providerStr := i.provider.String()
	requestTime := time.Now()

	requested := 1
	if params.N.Valid() {
		requested = int(params.N.Value)
	}

	resp, err := i.client.Images.NewVariation(ctx, params)
	if err != nil {
		duration := time.Since(requestTime)
		payload := i.buildErrorPayload(model, metadata, duration, providerStr, requestTime, err.Error())
		i.parent.metering.Send(payload)
		return nil, err
	}

	duration := time.Since(requestTime)
	actual := len(resp.Data)

	attrs := map[string]interface{}{
		"billing_unit":     "per_image",
		"operationSubtype": "variation",
	}
	if s := string(params.Size); s != "" {
		attrs["resolution"] = s
	}
	if rf := string(params.ResponseFormat); rf != "" {
		attrs["response_format"] = rf
	}

	payload := metering.NewPayload(metering.OperationImage, model, providerStr).
		WithTiming(requestTime, duration).
		WithImageBilling(actual, requested).
		WithAttributes(attrs).
		Build()

	metering.ApplyMetadata(payload, metadata)
	i.parent.metering.Send(payload)

	return resp, nil
}

func (i *ImagesInterface) buildErrorPayload(model string, md map[string]interface{}, duration time.Duration, provider string, requestTime time.Time, errorReason string) *metering.MeteringPayload {
	payload := metering.NewPayload(metering.OperationImage, model, provider).
		WithTiming(requestTime, duration).
		WithError(errorReason).
		Build()

	metering.ApplyMetadata(payload, md)
	return payload
}
