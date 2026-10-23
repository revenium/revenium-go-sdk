package openai

import (
	"errors"
	"time"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/packages/ssestream"
	"github.com/revenium/revenium-go-sdk/core"
	"github.com/revenium/revenium-go-sdk/core/metering"
)

// callFailure describes err for its usage record, reading the status and the
// error type and code of an openai-go API error, or of an error event the
// provider sent mid-stream, whose text is the provider's own.
func callFailure(err error) metering.CallFailure {
	var apiErr *openai.Error
	if errors.As(err, &apiErr) {
		return metering.ProviderFailure(err, apiErr.StatusCode, apiErr.RawJSON())
	}
	var streamErr *ssestream.StreamError
	if errors.As(err, &streamErr) {
		return metering.CallFailure{Err: err, Kind: core.ProviderErrorKind(streamErr.Event.Data), ProviderReply: true}
	}
	return metering.FailureOf(err)
}

func failedCallRecord(builder *metering.PayloadBuilder, err error, md map[string]interface{}) *metering.MeteringPayload {
	payload := builder.Build()
	metering.ApplyFailedCall(payload, callFailure(err), md)
	return payload
}

// applyStreamOutcome finishes a streamed call's record, which already holds
// the usage the provider reported, as a failure when the stream ended in one.
func applyStreamOutcome(payload *metering.MeteringPayload, streamErr error, md map[string]interface{}, capture func() metering.ChatCapture) {
	if streamErr != nil {
		metering.ApplyFailedCall(payload, callFailure(streamErr), md)
		return
	}
	metering.ApplyMetadata(payload, md)
	metering.ApplyChatCapture(payload, md, capture)
}

func chatFailureRecord(provider Provider, model string, md map[string]interface{}, isStreamed bool, requestTime time.Time, duration time.Duration, err error) *metering.MeteringPayload {
	builder := provider.record(metering.OperationChat, model).
		WithTiming(requestTime, duration).
		WithStreaming(isStreamed, 0, nil)
	return failedCallRecord(builder, err, md)
}
