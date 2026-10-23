package google

import (
	"context"
	"fmt"
	"time"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/revenium/revenium-go-sdk/core/metering"
	"google.golang.org/genai"
)

var videoPollInterval = 5 * time.Second

func (m *ModelsInterface) GenerateVideo(
	ctx context.Context,
	model string,
	prompt string,
	image *genai.Image,
	config *genai.GenerateVideosConfig,
) (*genai.GenerateVideosOperation, error) {
	core.Debug("GenerateVideo called with model: %s", model)

	if err := m.checkCostControls(ctx, model); err != nil {
		return nil, err
	}

	request := newVideoRequest(ctx, model, metering.SubtypeGeneration, config)
	ctx, sending := watchSending(ctx)
	op, err := m.client.Models.GenerateVideos(ctx, model, prompt, image, config)
	return m.trackSubmittedVideo(request, sending, op, err)
}

func (m *ModelsInterface) GenerateVideoFromSource(
	ctx context.Context,
	model string,
	source *genai.GenerateVideosSource,
	config *genai.GenerateVideosConfig,
) (*genai.GenerateVideosOperation, error) {
	core.Debug("GenerateVideoFromSource called with model: %s", model)

	if err := m.checkCostControls(ctx, model); err != nil {
		return nil, err
	}

	request := newVideoRequest(ctx, model, metering.SubtypeGeneration, config)
	ctx, sending := watchSending(ctx)
	op, err := m.client.Models.GenerateVideosFromSource(ctx, model, source, config)
	return m.trackSubmittedVideo(request, sending, op, err)
}

func (m *ModelsInterface) trackSubmittedVideo(
	request videoRequest,
	sending *sentRequest,
	op *genai.GenerateVideosOperation,
	err error,
) (*genai.GenerateVideosOperation, error) {
	videos := m.parent.videos
	if err != nil {
		core.Debug("Video submission error: %v", err)
		if !sending.left() {
			return nil, err
		}
		failure := callFailure(err)
		videos.report("", request, videoResult{failure: &failure})
		return nil, err
	}
	if op != nil {
		videos.track(op.Name, request)
		videos.settleOperation(op)
	}
	return op, nil
}

// WaitForVideo polls until the operation finishes. The usage record is sent once per
// operation when its outcome is first seen, so polling with
// Operations.GetVideosOperation instead records the same video exactly once.
func (m *ModelsInterface) WaitForVideo(
	ctx context.Context,
	model string,
	operation *genai.GenerateVideosOperation,
	operationSubtype string,
	config *genai.GenerateVideosConfig,
) (*genai.GenerateVideosOperation, error) {
	core.Debug("WaitForVideo polling for operation: %s", operation.Name)

	videos := m.parent.videos
	videos.trackWait(operation.Name, newVideoRequest(ctx, model, operationSubtype, config))

	result, err := m.pollVideoOperation(ctx, operation)
	videos.settleOperation(result)
	if err != nil {
		core.Debug("WaitForVideo error: %v", err)
		return nil, err
	}
	return result, nil
}

func (m *ModelsInterface) pollVideoOperation(
	ctx context.Context,
	operation *genai.GenerateVideosOperation,
) (*genai.GenerateVideosOperation, error) {
	ticker := time.NewTicker(videoPollInterval)
	defer ticker.Stop()

	for {
		if operation.Done {
			if operation.Error != nil {
				return operation, videoFailure(operation.Error).Err
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
