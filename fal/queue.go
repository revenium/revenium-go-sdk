package fal

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/revenium/revenium-go-sdk/core"
)

const defaultQueueBaseURL = "https://queue.fal.run"

// Subscribe enqueues a request, polls until completion, and returns the final result.
func (r *ReveniumFal) Subscribe(ctx context.Context, endpointID string, input map[string]interface{}, metadata map[string]interface{}) (map[string]interface{}, error) {
	contextMeta := core.GetUsageMetadata(ctx)
	mergedMeta := mergeMetadataMaps(contextMeta, metadata)
	startTime := time.Now()

	queueBase := r.queueBaseURL()
	body, err := json.Marshal(input)
	if err != nil {
		return nil, core.NewProviderError("failed to marshal input", err)
	}

	submitURL := endpointURL(queueBase, endpointID)
	submitBody, _, err := r.falClient.doRequest(ctx, "POST", submitURL, body, nil)
	if err != nil {
		return nil, err
	}

	var submit QueueSubmitResponse
	if err := json.Unmarshal(submitBody, &submit); err != nil {
		return nil, core.NewProviderError("failed to parse queue submit response", err)
	}
	if submit.RequestID == "" {
		return nil, core.NewProviderError("queue submit response missing request_id", nil)
	}

	if err := r.pollQueueStatus(ctx, queueBase, endpointID, submit.RequestID); err != nil {
		return nil, err
	}

	resultURL := fmt.Sprintf("%s/requests/%s", endpointURL(queueBase, endpointID), submit.RequestID)
	resultBody, _, err := r.falClient.doRequest(ctx, "GET", resultURL, nil, nil)
	if err != nil {
		return nil, err
	}

	result := map[string]interface{}{}
	if len(resultBody) > 0 {
		if jsonErr := json.Unmarshal(resultBody, &result); jsonErr != nil {
			return nil, core.NewProviderError("failed to parse queue result", jsonErr)
		}
	}

	duration := time.Since(startTime)
	r.meterResult(endpointID, result, mergedMeta, duration, startTime, input)
	return result, nil
}

func (r *ReveniumFal) pollQueueStatus(ctx context.Context, queueBase, endpointID, requestID string) error {
	statusURL := fmt.Sprintf("%s/requests/%s/status", endpointURL(queueBase, endpointID), requestID)
	backoff := 500 * time.Millisecond
	const maxBackoff = 5 * time.Second

	for {
		select {
		case <-ctx.Done():
			return core.NewNetworkError("queue polling cancelled", ctx.Err())
		default:
		}

		body, _, err := r.falClient.doRequest(ctx, "GET", statusURL, nil, nil)
		if err != nil {
			return err
		}

		var status QueueStatusResponse
		if err := json.Unmarshal(body, &status); err != nil {
			return core.NewProviderError("failed to parse queue status", err)
		}

		switch status.Status {
		case QueueCompleted:
			return nil
		case QueueFailed, QueueError:
			return core.NewProviderError(fmt.Sprintf("queue request %s ended with status %s", requestID, status.Status), nil)
		}

		select {
		case <-ctx.Done():
			return core.NewNetworkError("queue polling cancelled", ctx.Err())
		case <-time.After(backoff):
		}

		backoff *= 2
		if backoff > maxBackoff {
			backoff = maxBackoff
		}
	}
}

func (r *ReveniumFal) queueBaseURL() string {
	if r.config != nil && r.config.QueueBaseURL != "" {
		return r.config.QueueBaseURL
	}
	return defaultQueueBaseURL
}
