package openai

import (
	"context"
	"time"

	"github.com/openai/openai-go/v3"
	"github.com/revenium/revenium-go-sdk/core"
	"github.com/revenium/revenium-go-sdk/core/metering"
)

type EmbeddingsInterface struct {
	client   openai.Client
	config   *Config
	provider Provider
	parent   *ReveniumOpenAI
}

func (e *EmbeddingsInterface) Create(ctx context.Context, params openai.EmbeddingNewParams) (*openai.CreateEmbeddingResponse, error) {
	metadata := core.GetUsageMetadata(ctx)
	model := string(params.Model)
	providerStr := e.provider.String()
	requestTime := time.Now()

	resp, err := e.client.Embeddings.New(ctx, params)
	if err != nil {
		duration := time.Since(requestTime)
		payload := e.buildErrorPayload(model, metadata, duration, providerStr, requestTime, err.Error())
		e.parent.metering.Send(payload)
		return nil, err
	}

	duration := time.Since(requestTime)
	payload := e.buildResponsePayload(resp, metadata, duration, providerStr, requestTime)
	e.parent.metering.Send(payload)

	return resp, nil
}

func (e *EmbeddingsInterface) buildResponsePayload(resp *openai.CreateEmbeddingResponse, md map[string]interface{}, duration time.Duration, provider string, requestTime time.Time) *metering.MeteringPayload {
	payload := metering.NewPayload(metering.OperationEmbed, resp.Model, provider).
		WithTiming(requestTime, duration).
		WithTokens(resp.Usage.PromptTokens, 0, resp.Usage.TotalTokens).
		Build()

	metering.ApplyMetadata(payload, md)
	return payload
}

func (e *EmbeddingsInterface) buildErrorPayload(model string, md map[string]interface{}, duration time.Duration, provider string, requestTime time.Time, errorReason string) *metering.MeteringPayload {
	payload := metering.NewPayload(metering.OperationEmbed, model, provider).
		WithTiming(requestTime, duration).
		WithError(errorReason).
		Build()

	metering.ApplyMetadata(payload, md)
	return payload
}
