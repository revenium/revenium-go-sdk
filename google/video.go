package google

import (
	"context"
	"fmt"
	"time"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/revenium/revenium-go-sdk/core/metering"
	"google.golang.org/genai"
)

const defaultVideoPollInterval = 5 * time.Second

func (m *ModelsInterface) GenerateVideo(
	ctx context.Context,
	model string,
	prompt string,
	image *genai.Image,
	config *genai.GenerateVideosConfig,
) (*genai.GenerateVideosOperation, error) {
	metadata := core.GetUsageMetadata(ctx)

	core.Debug("GenerateVideo called with model: %s", model)

	requestTime := time.Now()

	op, err := m.client.Models.GenerateVideos(ctx, model, prompt, image, config)

	if err != nil {
		core.Debug("GenerateVideo error: %v", err)
		payload := metering.NewPayload(metering.OperationVideo, model, m.provider.String()).
			WithTiming(requestTime, time.Since(requestTime)).
			WithError(err.Error()).
			Build()
		metering.ApplyMetadata(payload, metadata)
		m.parent.metering.Send(payload)
		return nil, err
	}

	return op, nil
}

func (m *ModelsInterface) GenerateVideoFromSource(
	ctx context.Context,
	model string,
	source *genai.GenerateVideosSource,
	config *genai.GenerateVideosConfig,
) (*genai.GenerateVideosOperation, error) {
	metadata := core.GetUsageMetadata(ctx)

	core.Debug("GenerateVideoFromSource called with model: %s", model)

	requestTime := time.Now()

	op, err := m.client.Models.GenerateVideosFromSource(ctx, model, source, config)

	if err != nil {
		core.Debug("GenerateVideoFromSource error: %v", err)
		payload := metering.NewPayload(metering.OperationVideo, model, m.provider.String()).
			WithTiming(requestTime, time.Since(requestTime)).
			WithError(err.Error()).
			Build()
		metering.ApplyMetadata(payload, metadata)
		m.parent.metering.Send(payload)
		return nil, err
	}

	return op, nil
}

func (m *ModelsInterface) WaitForVideo(
	ctx context.Context,
	model string,
	operation *genai.GenerateVideosOperation,
	operationSubtype string,
	config *genai.GenerateVideosConfig,
) (*genai.GenerateVideosOperation, error) {
	metadata := core.GetUsageMetadata(ctx)

	core.Debug("WaitForVideo polling for operation: %s", operation.Name)

	requestTime := time.Now()

	result, err := m.pollVideoOperation(ctx, operation)

	duration := time.Since(requestTime)

	if err != nil {
		core.Debug("WaitForVideo error: %v", err)
		payload := metering.NewPayload(metering.OperationVideo, model, m.provider.String()).
			WithTiming(requestTime, duration).
			WithError(err.Error()).
			Build()
		metering.ApplyMetadata(payload, metadata)
		m.parent.metering.Send(payload)
		return nil, err
	}

	attrs := map[string]interface{}{
		"operationSubtype": operationSubtype,
	}

	var requestedDuration float64
	if config != nil {
		if config.DurationSeconds != nil {
			requestedDuration = float64(*config.DurationSeconds)
		}
		if config.AspectRatio != "" {
			attrs["resolution"] = mapAspectRatioToResolution(config.AspectRatio)
		}
	}
	if result.Response != nil {
		attrs["videoCount"] = len(result.Response.GeneratedVideos)
	}

	core.Debug("WaitForVideo completed in %v", duration)

	payload := metering.NewPayload(metering.OperationVideo, model, m.provider.String()).
		WithTiming(requestTime, duration).
		WithVideoDuration(0, requestedDuration).
		WithAttributes(attrs).
		Build()
	metering.ApplyMetadata(payload, metadata)
	m.parent.metering.Send(payload)

	return result, nil
}

func (m *ModelsInterface) pollVideoOperation(
	ctx context.Context,
	operation *genai.GenerateVideosOperation,
) (*genai.GenerateVideosOperation, error) {
	ticker := time.NewTicker(defaultVideoPollInterval)
	defer ticker.Stop()

	for {
		if operation.Done {
			if operation.Error != nil {
				return operation, fmt.Errorf("video generation failed: %v", operation.Error)
			}
			return operation, nil
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
			updated, err := m.client.Operations.GetVideosOperation(ctx, operation, nil)
			if err != nil {
				return nil, fmt.Errorf("failed to poll video operation: %w", err)
			}
			operation = updated
		}
	}
}
