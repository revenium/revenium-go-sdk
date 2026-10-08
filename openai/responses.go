package openai

import (
	"context"
	"sync"
	"time"

	"github.com/openai/openai-go/v3/packages/ssestream"
	"github.com/openai/openai-go/v3/responses"
	"github.com/revenium/revenium-go-sdk/core"
	"github.com/revenium/revenium-go-sdk/core/metering"
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
	providerStr := r.provider.String()
	requestTime := time.Now()

	resp, err := r.client.New(ctx, params)
	if err != nil {
		duration := time.Since(requestTime)
		payload := r.buildErrorPayload(model, metadata, duration, providerStr, requestTime, err.Error())
		r.parent.metering.Send(payload)
		return nil, err
	}

	duration := time.Since(requestTime)
	payload := r.buildResponsePayload(resp, metadata, duration, providerStr, requestTime, nil)
	r.parent.metering.Send(payload)

	return resp, nil
}

func (r *ResponsesInterface) CreateStreaming(ctx context.Context, params responses.ResponseNewParams) (*ResponsesStreamingWrapper, error) {
	metadata := core.GetUsageMetadata(ctx)
	model := string(params.Model)

	startTime := time.Now()
	stream := r.client.NewStreaming(ctx, params)

	streamMetadata := make(map[string]interface{})
	if metadata != nil {
		for k, v := range metadata {
			streamMetadata[k] = v
		}
	}

	return &ResponsesStreamingWrapper{
		stream:    stream,
		config:    r.config,
		metadata:  streamMetadata,
		startTime: startTime,
		iface:     r,
		model:     model,
		provider:  r.provider.String(),
		parent:    r.parent,
	}, nil
}

type streamTiming struct {
	timeToFirstToken    int64
	completionStartTime *time.Time
}

func (r *ResponsesInterface) buildResponsePayload(resp *responses.Response, md map[string]interface{}, duration time.Duration, provider string, requestTime time.Time, stream *streamTiming) *metering.MeteringPayload {
	inputTokens := resp.Usage.InputTokens
	outputTokens := resp.Usage.OutputTokens
	totalTokens := resp.Usage.TotalTokens
	reasoningTokens := resp.Usage.OutputTokensDetails.ReasoningTokens
	cachedTokens := resp.Usage.InputTokensDetails.CachedTokens

	timeToFirstToken := int64(0)
	var completionStartTime *time.Time
	if stream != nil {
		timeToFirstToken = stream.timeToFirstToken
		completionStartTime = stream.completionStartTime
	}

	payload := metering.NewPayload(metering.OperationChat, string(resp.Model), provider).
		WithTiming(requestTime, duration).
		WithTokens(inputTokens, outputTokens, totalTokens).
		WithReasoningTokens(reasoningTokens, 0, cachedTokens).
		WithStreaming(stream != nil, timeToFirstToken, completionStartTime).
		WithStopReason(mapResponseStopReason(resp)).
		Build()

	metering.ApplyMetadata(payload, md)
	return payload
}

func (r *ResponsesInterface) buildErrorPayload(model string, md map[string]interface{}, duration time.Duration, provider string, requestTime time.Time, errorReason string) *metering.MeteringPayload {
	payload := metering.NewPayload(metering.OperationChat, model, provider).
		WithTiming(requestTime, duration).
		WithError(errorReason).
		Build()

	metering.ApplyMetadata(payload, md)
	return payload
}

type ResponsesStreamingWrapper struct {
	stream    *ssestream.Stream[responses.ResponseStreamEventUnion]
	config    *Config
	metadata  map[string]interface{}
	startTime time.Time
	iface     *ResponsesInterface
	model     string
	provider  string
	parent    *ReveniumOpenAI
	mu        sync.Mutex

	firstTokenTime *time.Time
	finalResponse  *responses.Response
}

func (sw *ResponsesStreamingWrapper) Next() bool {
	return sw.stream.Next()
}

func (sw *ResponsesStreamingWrapper) Current() responses.ResponseStreamEventUnion {
	event := sw.stream.Current()
	sw.captureEvent(event)
	return event
}

func (sw *ResponsesStreamingWrapper) captureEvent(event responses.ResponseStreamEventUnion) {
	sw.mu.Lock()
	defer sw.mu.Unlock()

	if sw.firstTokenTime == nil && event.Type == "response.output_text.delta" {
		now := time.Now()
		sw.firstTokenTime = &now
	}

	if event.Type == "response.completed" {
		resp := event.Response
		sw.finalResponse = &resp
	}
}

func (sw *ResponsesStreamingWrapper) Err() error {
	return sw.stream.Err()
}

func (sw *ResponsesStreamingWrapper) Close() error {
	err := sw.stream.Close()
	streamErr := sw.stream.Err()
	duration := time.Since(sw.startTime)

	sw.mu.Lock()
	defer sw.mu.Unlock()

	if streamErr != nil {
		payload := sw.iface.buildErrorPayload(sw.model, sw.metadata, duration, sw.provider, sw.startTime, streamErr.Error())
		sw.parent.metering.Send(payload)
		return err
	}

	timeToFirstToken := int64(0)
	var completionStartTime *time.Time
	if sw.firstTokenTime != nil {
		timeToFirstToken = sw.firstTokenTime.Sub(sw.startTime).Milliseconds()
		completionStartTime = sw.firstTokenTime
	}

	if sw.finalResponse != nil {
		timing := &streamTiming{timeToFirstToken: timeToFirstToken, completionStartTime: completionStartTime}
		payload := sw.iface.buildResponsePayload(sw.finalResponse, sw.metadata, duration, sw.provider, sw.startTime, timing)
		sw.parent.metering.Send(payload)
	} else {
		payload := metering.NewPayload(metering.OperationChat, sw.model, sw.provider).
			WithTiming(sw.startTime, duration).
			WithStreaming(true, timeToFirstToken, completionStartTime).
			Build()

		metering.ApplyMetadata(payload, sw.metadata)
		sw.parent.metering.Send(payload)
	}

	return err
}

func mapResponseStopReason(resp *responses.Response) string {
	if resp.Status == "completed" && hasToolCallOutput(resp.Output) {
		return "END_SEQUENCE"
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
