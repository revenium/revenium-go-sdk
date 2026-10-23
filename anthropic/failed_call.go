package anthropic

import (
	"errors"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/revenium/revenium-go-sdk/core/metering"
)

// streamErrorEventPrefix starts the plain error anthropic-sdk-go returns for
// an error event mid-stream whose body is not an Anthropic error object
// (packages/ssestream); the rest of its text is the event body.
const streamErrorEventPrefix = "received error while streaming: "

// callFailure describes err for its usage record, reading the status and
// error type of an Anthropic API error, or of an error event the SDK passes
// on as plain text, which is the provider's own reply; AWS SDK errors carry
// theirs.
func callFailure(err error) metering.CallFailure {
	var apiErr *anthropic.Error
	if errors.As(err, &apiErr) {
		return metering.ProviderFailure(err, apiErr.StatusCode, apiErr.RawJSON())
	}
	if event, found := strings.CutPrefix(err.Error(), streamErrorEventPrefix); found {
		return metering.CallFailure{Err: err, Kind: core.ProviderErrorKind([]byte(event)), ProviderReply: true}
	}
	return metering.FailureOf(err)
}

func buildFailedTurnPayload(turn turnUsage, timing turnTiming, provider string, hasVision bool, md map[string]interface{}, err error) *metering.MeteringPayload {
	payload := turnRecord(turn, timing, provider, hasVision)
	metering.ApplyFailedCall(payload, callFailure(err), md)
	return payload
}

func sendFailedCall(mc *metering.MeteringClient, model, provider string, start time.Time, hasVision bool, md map[string]interface{}, err error) {
	timing := turnTiming{start: start, duration: time.Since(start)}
	mc.Send(buildFailedTurnPayload(turnUsage{model: model}, timing, provider, hasVision, md, err))
}

// requestSeen tells whether a tool runner turn sent a model request, so a
// turn that failed in a tool, before any request, is not recorded as a
// failed call.
type requestSeen struct{ sent atomic.Bool }

func (s *requestSeen) option() option.RequestOption {
	return option.WithMiddleware(func(req *http.Request, next option.MiddlewareNext) (*http.Response, error) {
		s.sent.Store(true)
		return next(req)
	})
}
