package openai

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/openai/openai-go/v3/packages/ssestream"
	"github.com/openai/openai-go/v3/responses"
	"github.com/revenium/revenium-go-sdk/core"
	"github.com/revenium/revenium-go-sdk/core/metering"
	"github.com/revenium/revenium-go-sdk/core/prompt"
)

type ResponsesInterface struct {
	client   responses.ResponseService
	config   *Config
	provider Provider
	parent   *ReveniumOpenAI
}

func (r *ResponsesInterface) Create(ctx context.Context, params responses.ResponseNewParams) (*responses.Response, error) {
	metadata := core.GetUsageMetadata(ctx)
	model := string(params.Model)
	if err := r.parent.checkCostControls(ctx, model, r.provider, true); err != nil {
		core.Debug("Responses.Create blocked by enforcement: %v", err)
		return nil, err
	}
	requestTime := time.Now()

	resp, err := r.client.New(ctx, params)
	if err != nil {
		duration := time.Since(requestTime)
		r.parent.metering.Send(chatFailureRecord(r.provider, model, metadata, false, requestTime, duration, err))
		return nil, err
	}

	duration := time.Since(requestTime)
	payload := r.buildResponsePayload(resp, model, metadata, duration, r.provider, requestTime, nil)
	applyResponsesCapture(payload, metadata, params, resp)
	r.parent.metering.Send(payload)

	return resp, nil
}

func (r *ResponsesInterface) CreateStreaming(ctx context.Context, params responses.ResponseNewParams) (*ResponsesStreamingWrapper, error) {
	metadata := core.GetUsageMetadata(ctx)
	model := string(params.Model)
	if err := r.parent.checkCostControls(ctx, model, r.provider, true); err != nil {
		core.Debug("Responses.CreateStreaming blocked by enforcement: %v", err)
		return nil, err
	}

	clock := startStreamClock()
	stream := r.client.NewStreaming(ctx, params)

	streamMetadata := make(map[string]interface{})
	if metadata != nil {
		for k, v := range metadata {
			streamMetadata[k] = v
		}
	}

	return &ResponsesStreamingWrapper{
		stream:   stream,
		config:   r.config,
		metadata: streamMetadata,
		clock:    clock,
		iface:    r,
		model:    model,
		provider: r.provider,
		parent:   r.parent,
		params:   params,
		reply:    metering.ReplyCollectorFor(streamMetadata),
	}, nil
}

type streamTiming struct {
	timeToFirstToken    int64
	completionStartTime *time.Time
}

func (r *ResponsesInterface) buildResponsePayload(resp *responses.Response, requestedModel string, md map[string]interface{}, duration time.Duration, provider Provider, requestTime time.Time, stream *streamTiming) *metering.MeteringPayload {
	timeToFirstToken := int64(0)
	var completionStartTime *time.Time
	if stream != nil {
		timeToFirstToken = stream.timeToFirstToken
		completionStartTime = stream.completionStartTime
	}

	builder := responseTokenUsage(resp.Usage).chatRecord(provider, servedOrRequested(string(resp.Model), requestedModel)).
		WithTiming(requestTime, duration).
		WithStreaming(stream != nil, timeToFirstToken, completionStartTime).
		WithStopReason(mapResponseStopReason(resp))
	payload := builder.Build()
	if resp.Status == responses.ResponseStatusFailed {
		metering.ApplyFailedCall(payload, responseFailure(resp.Error), md)
		return payload
	}
	metering.ApplyMetadata(payload, md)
	return payload
}

// responseFailure names a failed response by its code. The message is the
// provider's own text, which can quote the request.
func responseFailure(failure responses.ResponseError) metering.CallFailure {
	kind := core.ErrorKindFrom(string(failure.Code))
	if kind == "" {
		kind = "response_failed"
	}
	message := failure.Message
	if message == "" {
		message = "response failed"
	}
	return metering.CallFailure{Err: errors.New(message), Kind: kind, ProviderReply: true}
}

type ResponsesStreamingWrapper struct {
	stream   *ssestream.Stream[responses.ResponseStreamEventUnion]
	config   *Config
	metadata map[string]interface{}
	clock    streamClock
	iface    *ResponsesInterface
	model    string
	provider Provider
	parent   *ReveniumOpenAI
	mu       sync.Mutex
	closed   bool

	finalResponse *responses.Response
	params        responses.ResponseNewParams
	reply         *prompt.ReplyCollector
}

func (sw *ResponsesStreamingWrapper) Next() bool {
	if !sw.stream.Next() {
		sw.markExhausted()
		return false
	}
	sw.captureEvent(sw.stream.Current())
	return true
}

func (sw *ResponsesStreamingWrapper) markExhausted() {
	sw.mu.Lock()
	defer sw.mu.Unlock()
	sw.clock.markExhausted()
}

func (sw *ResponsesStreamingWrapper) Current() responses.ResponseStreamEventUnion {
	return sw.stream.Current()
}

func (sw *ResponsesStreamingWrapper) captureEvent(event responses.ResponseStreamEventUnion) {
	sw.mu.Lock()
	defer sw.mu.Unlock()

	sw.clock.observe(isOutputDeltaEvent(event), responseProgress(event))
	if event.Type == "response.output_text.delta" {
		sw.reply.Add(event.Delta)
	}

	if isFinalResponseEvent(event.Type) {
		resp := event.Response
		sw.finalResponse = &resp
	}
}

func isOutputDeltaEvent(event responses.ResponseStreamEventUnion) bool {
	switch event.Type {
	case "response.output_text.delta", "response.refusal.delta", "response.function_call_arguments.delta",
		"response.custom_tool_call_input.delta", "response.audio.delta", "response.audio.transcript.delta":
		return event.Delta != ""
	default:
		return false
	}
}

func responseProgress(event responses.ResponseStreamEventUnion) choiceProgress {
	if isFinalResponseEvent(event.Type) {
		return choiceProgress{0: true}
	}
	return nil
}

func isFinalResponseEvent(eventType string) bool {
	switch eventType {
	case "response.completed", "response.incomplete", "response.failed":
		return true
	default:
		return false
	}
}

func (sw *ResponsesStreamingWrapper) Err() error {
	return sw.stream.Err()
}

func (sw *ResponsesStreamingWrapper) Close() error {
	sw.mu.Lock()
	defer sw.mu.Unlock()

	if sw.closed {
		return nil
	}
	sw.closed = true

	err := sw.stream.Close()
	streamErr := sw.stream.Err()
	duration := sw.clock.duration()

	if streamErr != nil {
		sw.parent.metering.Send(chatFailureRecord(sw.provider, sw.model, sw.metadata, true, sw.clock.start, duration, streamErr))
		return err
	}

	timeToFirstToken := sw.clock.timeToFirstToken()
	completionStartTime := sw.clock.firstToken

	if sw.finalResponse != nil {
		timing := &streamTiming{timeToFirstToken: timeToFirstToken, completionStartTime: completionStartTime}
		payload := sw.iface.buildResponsePayload(sw.finalResponse, sw.model, sw.metadata, duration, sw.provider, sw.clock.start, timing)
		applyResponsesCapture(payload, sw.metadata, sw.params, sw.finalResponse)
		sw.parent.metering.Send(payload)
	} else {
		payload := sw.provider.record(metering.OperationChat, sw.model).
			WithTiming(sw.clock.start, duration).
			WithStreaming(true, timeToFirstToken, completionStartTime).
			Build()

		metering.ApplyMetadata(payload, sw.metadata)
		metering.ApplyChatCapture(payload, sw.metadata, func() metering.ChatCapture {
			return responsesCapture(sw.params, "").WithStreamedReply(sw.reply)
		})
		sw.parent.metering.Send(payload)
	}

	return err
}

func mapResponseStopReason(resp *responses.Response) string {
	if resp.Status == "completed" && hasToolCallOutput(resp.Output) {
		return "END_SEQUENCE"
	}
	if resp.Status == "incomplete" && resp.IncompleteDetails.Reason == "content_filter" {
		return string(core.StopReasonError)
	}
	return mapResponseStatus(string(resp.Status))
}

func hasToolCallOutput(output []responses.ResponseOutputItemUnion) bool {
	for _, item := range output {
		if item.Type == "function_call" || item.Type == "custom_tool_call" {
			return true
		}
	}
	return false
}

func mapResponseStatus(status string) string {
	switch status {
	case "completed":
		return "END"
	case "failed":
		return "ERROR"
	case "cancelled":
		return "CANCELLED"
	case "incomplete":
		return "TOKEN_LIMIT"
	default:
		return "END"
	}
}

func applyResponsesCapture(payload *metering.MeteringPayload, metadata map[string]interface{}, params responses.ResponseNewParams, resp *responses.Response) {
	if resp.Status == responses.ResponseStatusFailed {
		return
	}
	metering.ApplyChatCapture(payload, metadata, func() metering.ChatCapture { return responsesCapture(params, resp.OutputText()) })
}
