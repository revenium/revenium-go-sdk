package perplexity

import (
	"errors"

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
