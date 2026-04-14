package fal

import (
	"context"
	"encoding/json"
	"time"

	"github.com/revenium/revenium-go-sdk/core"
)

// Run executes a synchronous fal request, auto-detecting media type and emitting metering accordingly.
// endpointID is used literally (e.g., "fal-ai/flux/dev"); no prefix is stripped or added.
func (r *ReveniumFal) Run(ctx context.Context, endpointID string, input map[string]interface{}, metadata map[string]interface{}) (map[string]interface{}, error) {
	contextMeta := core.GetUsageMetadata(ctx)
	mergedMeta := mergeMetadataMaps(contextMeta, metadata)
	startTime := time.Now()

	body, err := json.Marshal(input)
	if err != nil {
		return nil, core.NewProviderError("failed to marshal input", err)
	}

	url := endpointURL(r.config.FalBaseURL, endpointID)
	respBody, _, err := r.falClient.doRequest(ctx, "POST", url, body, nil)
	duration := time.Since(startTime)

	if err != nil {
		return nil, err
	}

	result := map[string]interface{}{}
	if len(respBody) > 0 {
		if jsonErr := json.Unmarshal(respBody, &result); jsonErr != nil {
			return nil, core.NewProviderError("failed to parse response", jsonErr)
		}
	}

	r.meterResult(endpointID, result, mergedMeta, duration, startTime, input)
	return result, nil
}

func (r *ReveniumFal) meterResult(endpointID string, result map[string]interface{}, metadata map[string]interface{}, duration time.Duration, startTime time.Time, input map[string]interface{}) {
	if !r.enabled.Load() {
		return
	}

	op := DetectMediaType(endpointID, result)
	prompt := promptFromInput(input)
	payload := buildPayloadFromResult(endpointID, op, result, input, metadata, duration, startTime, r.config.CapturePrompts, prompt)
	if payload != nil {
		r.metering.Send(payload)
	}
}

func promptFromInput(input map[string]interface{}) string {
	if input == nil {
		return ""
	}
	if v, ok := input["prompt"].(string); ok {
		return v
	}
	if nested, ok := input["input"].(map[string]interface{}); ok {
		if v, ok := nested["prompt"].(string); ok {
			return v
		}
	}
	return ""
}

func mergeMetadataMaps(contextMeta, argMeta map[string]interface{}) map[string]interface{} {
	if len(contextMeta) == 0 && len(argMeta) == 0 {
		return nil
	}
	merged := make(map[string]interface{}, len(contextMeta)+len(argMeta))
	for k, v := range argMeta {
		merged[k] = v
	}
	for k, v := range contextMeta {
		merged[k] = v
	}
	return merged
}
