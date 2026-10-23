package fal

import (
	"context"
	"encoding/json"
	"time"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/revenium/revenium-go-sdk/core/enforcement"
	"github.com/revenium/revenium-go-sdk/core/prompt"
)

// Run executes a synchronous fal request, auto-detecting media type and emitting metering accordingly.
// endpointID is used literally (e.g., "fal-ai/flux/dev"); no prefix is stripped or added.
// When the usage metadata on ctx and the metadata argument set the same field, the context value wins.
func (r *ReveniumFal) Run(ctx context.Context, endpointID string, input map[string]interface{}, metadata map[string]interface{}) (map[string]interface{}, error) {
	mergedMeta := core.UsageMetadataOver(ctx, metadata)
	if err := r.enforcer.Check(evalContext(ctx, mergedMeta, endpointID)); err != nil {
		core.Debug("Run blocked by enforcement: %v", err)
		return nil, err
	}

	startTime := time.Now()

	body, err := json.Marshal(input)
	if err != nil {
		return nil, core.NewProviderError("failed to marshal input", err)
	}

	url := endpointURL(r.config.FalBaseURL, endpointID)
	respBody, _, err := r.falClient.doRequest(ctx, "POST", url, body, nil)
	duration := time.Since(startTime)

	if err != nil {
		r.recordFailure(endpointID, mergedMeta, startTime, false, err)
		return nil, err
	}

	result := map[string]interface{}{}
	if len(respBody) > 0 {
		if jsonErr := json.Unmarshal(respBody, &result); jsonErr != nil {
			err = core.NewProviderError("failed to parse response", jsonErr)
			r.recordFailure(endpointID, mergedMeta, startTime, false, err)
			return nil, err
		}
	}

	r.meterResult(endpointID, result, mergedMeta, duration, startTime, input)
	return result, nil
}

func (r *ReveniumFal) meterResult(endpointID string, result map[string]interface{}, metadata map[string]interface{}, duration time.Duration, startTime time.Time, input map[string]interface{}) {
	if !r.enabled.Load() {
		return
	}
	r.sendResultRecord(endpointID, result, metadata, duration, startTime, input)
}

func (r *ReveniumFal) sendResultRecord(endpointID string, result map[string]interface{}, metadata map[string]interface{}, duration time.Duration, startTime time.Time, input map[string]interface{}) {
	fields := meteredInputFields(input)
	op := DetectMediaType(endpointID, result)
	r.rememberOperation(endpointID, op)
	payload := buildPayloadFromResult(endpointID, op, result, fields, metadata, duration, startTime, prompt.CapturesFor(metadata, r.config.CapturePrompts), promptFromInput(fields))
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

func evalContext(ctx context.Context, mergedMeta map[string]interface{}, endpointID string) enforcement.EvalContext {
	return enforcement.NewEvalContextFromMetadata(ctx, mergedMeta, normalizeModelName(endpointID), "fal_ai")
}
