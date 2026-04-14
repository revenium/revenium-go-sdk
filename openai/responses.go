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
	payload := r.buildResponsePayload(resp, metadata, duration, providerStr, requestTime, false)
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

func (r *ResponsesInterface) buildResponsePayload(resp *responses.Response, md map[string]interface{}, duration time.Duration, provider string, requestTime time.Time, isStreamed bool) *metering.MeteringPayload {
	inputTokens := resp.Usage.InputTokens
	outputTokens := resp.Usage.OutputTokens
	totalTokens := resp.Usage.TotalTokens
	reasoningTokens := resp.Usage.OutputTokensDetails.ReasoningTokens
	cachedTokens := resp.Usage.InputTokensDetails.CachedTokens

	payload := metering.NewPayload(metering.OperationChat, string(resp.Model), provider).
		WithTiming(requestTime, duration).
		WithTokens(inputTokens, outputTokens, totalTokens).
		WithReasoningTokens(reasoningTokens, 0, cachedTokens).
		WithStreaming(isStreamed, 0, nil).
		WithStopReason(mapResponseStatus(string(resp.Status))).
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

	return event
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
		resp := sw.finalResponse
		payload := metering.NewPayload(metering.OperationChat, string(resp.Model), sw.provider).
			WithTiming(sw.startTime, duration).
			WithTokens(resp.Usage.InputTokens, resp.Usage.OutputTokens, resp.Usage.TotalTokens).
			WithReasoningTokens(resp.Usage.OutputTokensDetails.ReasoningTokens, 0, resp.Usage.InputTokensDetails.CachedTokens).
			WithStreaming(true, timeToFirstToken, completionStartTime).
			WithStopReason(mapResponseStatus(string(resp.Status))).
			Build()

		metering.ApplyMetadata(payload, sw.metadata)
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
