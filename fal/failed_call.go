package fal

import (
	"time"

	"github.com/revenium/revenium-go-sdk/core/metering"
	"github.com/revenium/revenium-go-sdk/core/prompt"
)

// recordFailure sends the one record of a fal request that failed, when
// metering is on.
func (r *ReveniumFal) recordFailure(endpointID string, metadata map[string]interface{}, startTime time.Time, isStreamed bool, err error) {
	if r.enabled.Load() {
		r.sendFailureRecord(endpointID, metadata, startTime, isStreamed, err)
	}
}

// sendFailureRecord sends the record without checking whether metering is
// on, for a stream that took its snapshot when it started. fal does not bill
// a failed request, so the record carries no quantity.
func (r *ReveniumFal) sendFailureRecord(endpointID string, metadata map[string]interface{}, startTime time.Time, isStreamed bool, err error) {
	op := r.operationOf(endpointID)
	payload := metering.NewPayload(op, normalizeModelName(endpointID), "fal_ai").
		WithTiming(startTime, time.Since(startTime)).
		WithModelSource("FAL").
		WithOperationSubtype(mediaSubtype(op, endpointID)).
		WithStreaming(isStreamed, 0, nil).
		Build()
	metering.ApplyFailedCall(payload, metering.FailureOf(err), prompt.WithClientCaptureSetting(metadata, r.config.CapturePrompts))
	r.metering.Send(payload)
}

// operationOf is the operation type an endpoint's replies resolved to, or,
// before any reply, the one its id suggests.
func (r *ReveniumFal) operationOf(endpointID string) metering.OperationType {
	r.operationsMu.RLock()
	op, ok := r.operations[endpointID]
	r.operationsMu.RUnlock()
	if ok {
		return op
	}
	return DetectMediaType(endpointID, nil)
}

func (r *ReveniumFal) rememberOperation(endpointID string, op metering.OperationType) {
	r.operationsMu.Lock()
	defer r.operationsMu.Unlock()
	if r.operations == nil {
		r.operations = map[string]metering.OperationType{}
	}
	r.operations[endpointID] = op
}

func mediaSubtype(op metering.OperationType, endpointID string) string {
	switch op {
	case metering.OperationImage:
		return detectImageSubtype(endpointID)
	case metering.OperationVideo:
		return detectVideoSubtype(endpointID)
	case metering.OperationAudio:
		return detectAudioSubtype(endpointID)
	default:
		return ""
	}
}
