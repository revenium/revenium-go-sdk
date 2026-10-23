package google

import (
	"context"
	"math"
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

	if err := m.checkCostControls(ctx, model); err != nil {
		core.Debug("CreateEmbedding blocked by enforcement: %v", err)
		return nil, err
	}

	requestTime := time.Now()
	ctx, sending := watchSending(ctx)

	resp, err := m.client.Models.EmbedContent(ctx, model, contents, config)

	duration := time.Since(requestTime)

	if err != nil {
		core.Debug("CreateEmbedding error: %v", err)
		if !sending.left() {
			return nil, err
		}
		payload := metering.NewPayload(metering.OperationEmbed, model, m.provider.String()).
			WithTiming(requestTime, duration).
			Build()
		metering.ApplyFailedCall(payload, callFailure(err), metadata)
		m.parent.metering.Send(payload)
		return nil, err
	}

	attrs := map[string]interface{}{}
	var billableCharacters int64
	if resp.Metadata != nil {
		billableCharacters = int64(resp.Metadata.BillableCharacterCount)
		attrs["billableCharacterCount"] = billableCharacters
	}

	inputTokens := embeddingInputTokens(resp)

	core.Debug("CreateEmbedding completed in %v, input tokens: %d, billable characters: %d", duration, inputTokens, billableCharacters)

	payload := metering.NewPayload(metering.OperationEmbed, model, m.provider.String()).
		WithTiming(requestTime, duration).
		WithTokens(inputTokens, 0, inputTokens).
		WithAttributes(attrs).
		Build()
	metering.ApplyMetadata(payload, metadata)
	m.parent.metering.Send(payload)

	return resp, nil
}

// Only Vertex AI reports token counts for embeddings, one per embedding; the
// Gemini API reports none, so its records keep zero tokens.
func embeddingInputTokens(resp *genai.EmbedContentResponse) int64 {
	var total float64
	for _, embedding := range resp.Embeddings {
		if embedding != nil && embedding.Statistics != nil {
			total += float64(embedding.Statistics.TokenCount)
		}
	}
	return int64(math.Round(total))
}
