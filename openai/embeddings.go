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
	if err := e.parent.checkCostControls(ctx, model, e.provider, true); err != nil {
		core.Debug("Embeddings.Create blocked by enforcement: %v", err)
		return nil, err
	}
	requestTime := time.Now()

	resp, err := e.client.Embeddings.New(ctx, params)
	if err != nil {
		duration := time.Since(requestTime)
		builder := e.provider.record(metering.OperationEmbed, model).WithTiming(requestTime, duration)
		e.parent.metering.Send(failedCallRecord(builder, err, metadata))
		return nil, err
	}

	duration := time.Since(requestTime)
	payload := e.buildResponsePayload(resp, metadata, duration, e.provider, requestTime)
	e.parent.metering.Send(payload)

	return resp, nil
}

func (e *EmbeddingsInterface) buildResponsePayload(resp *openai.CreateEmbeddingResponse, md map[string]interface{}, duration time.Duration, provider Provider, requestTime time.Time) *metering.MeteringPayload {
	payload := provider.record(metering.OperationEmbed, resp.Model).
		WithTiming(requestTime, duration).
		WithTokens(resp.Usage.PromptTokens, 0, resp.Usage.TotalTokens).
		Build()

	metering.ApplyMetadata(payload, md)
	return payload
}
