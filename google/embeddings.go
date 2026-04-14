package google

import (
	"context"
	"time"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/revenium/revenium-go-sdk/core/metering"
	"google.golang.org/genai"
)

func (m *ModelsInterface) CreateEmbedding(
	ctx context.Context,
	model string,
	contents []*genai.Content,
	config *genai.EmbedContentConfig,
) (*genai.EmbedContentResponse, error) {
	metadata := core.GetUsageMetadata(ctx)

	core.Debug("CreateEmbedding called with model: %s", model)

	requestTime := time.Now()

	resp, err := m.client.Models.EmbedContent(ctx, model, contents, config)

	duration := time.Since(requestTime)

	if err != nil {
		core.Debug("CreateEmbedding error: %v", err)
		payload := metering.NewPayload(metering.OperationEmbed, model, m.provider.String()).
			WithTiming(requestTime, duration).
			WithError(err.Error()).
			Build()
		metering.ApplyMetadata(payload, metadata)
		m.parent.metering.Send(payload)
		return nil, err
	}

	attrs := map[string]interface{}{}
	var billableCharacters int64
	if resp.Metadata != nil {
		billableCharacters = int64(resp.Metadata.BillableCharacterCount)
		attrs["billableCharacterCount"] = billableCharacters
	}

	core.Debug("CreateEmbedding completed in %v, billable characters: %d", duration, billableCharacters)

	payload := metering.NewPayload(metering.OperationEmbed, model, m.provider.String()).
		WithTiming(requestTime, duration).
		WithAttributes(attrs).
		Build()
	metering.ApplyMetadata(payload, metadata)
	m.parent.metering.Send(payload)

	return resp, nil
}
