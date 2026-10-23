package litellm

import (
	"time"

	"github.com/revenium/revenium-go-sdk/core/metering"
)

func failedCallRecord(op metering.OperationType, model string, isStreamed bool, requestTime time.Time, err error, metadata map[string]interface{}) *metering.MeteringPayload {
	payload := metering.NewPayload(op, ExtractModelName(model), ExtractProvider(model)).
		WithTiming(requestTime, time.Since(requestTime)).
		WithStreaming(isStreamed, 0, nil).
		WithModelSource(ExtractModelSource(model)).
		Build()
	metering.ApplyFailedCall(payload, metering.FailureOf(err), metadata)
	return payload
}
