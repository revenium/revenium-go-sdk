package openai

import (
	"context"
	"sync"
	"time"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/packages/ssestream"
	"github.com/revenium/revenium-go-sdk/core"
	"github.com/revenium/revenium-go-sdk/core/metering"
	"github.com/revenium/revenium-go-sdk/core/prompt"
)

type TextCompletionsInterface struct {
	chat *CompletionsInterface
}

func (r *ReveniumOpenAI) Completions() *TextCompletionsInterface {
	return &TextCompletionsInterface{chat: r.Chat().Completions()}
}

func (t *TextCompletionsInterface) New(ctx context.Context, params openai.CompletionNewParams) (*openai.Completion, error) {
	if err := t.checkCostControls(ctx, string(params.Model)); err != nil {
		return nil, err
	}
	metadata := core.GetUsageMetadata(ctx)
	model := string(params.Model)
	provider := t.chat.provider
	requestTime := time.Now()

	resp, err := t.chat.client.Completions.New(ctx, params)
	duration := time.Since(requestTime)
	if err != nil {
		t.chat.parent.metering.Send(chatFailureRecord(provider, model, metadata, false, requestTime, duration, err))
		return nil, err
	}

	payload := completionUsagePayload(provider.chatRecordModel(model, resp.Model), provider, resp.Usage, textFinishReason(*resp), resp.SystemFingerprint).
		WithTiming(requestTime, duration).
		Build()
	metering.ApplyMetadata(payload, metadata)
	metering.ApplyChatCapture(payload, metadata, func() metering.ChatCapture { return textCompletionCapture(params, textCompletionReply(*resp)) })
	t.chat.parent.metering.Send(payload)
	return resp, nil
}

func (t *TextCompletionsInterface) NewStreaming(ctx context.Context, params openai.CompletionNewParams) (*TextCompletionStream, error) {
	if err := t.checkCostControls(ctx, string(params.Model)); err != nil {
		return nil, err
	}
	streamOptions, callerRequestedUsage := withUsageReporting(params.StreamOptions)
	params.StreamOptions = streamOptions

	metadata := core.GetUsageMetadata(ctx)
	clock := startStreamClock()
	return &TextCompletionStream{
		stream:         t.chat.client.Completions.NewStreaming(ctx, params),
		completions:    t,
		metadata:       metadata,
		model:          string(params.Model),
		clock:          clock,
		hideUsageChunk: !callerRequestedUsage,
		params:         params,
		reply:          metering.ReplyCollectorFor(metadata),
	}, nil
}

func (t *TextCompletionsInterface) checkCostControls(ctx context.Context, model string) error {
	provider := t.chat.provider
	if err := t.chat.parent.checkCostControls(ctx, model, provider, provider.recordsServedChatModel()); err != nil {
		core.Debug("Completions blocked by enforcement: %v", err)
		return err
	}
	return nil
}

func textFinishReason(completion openai.Completion) string {
	if len(completion.Choices) == 0 {
		return ""
	}
	return string(completion.Choices[0].FinishReason)
}

type TextCompletionStream struct {
	stream         *ssestream.Stream[openai.Completion]
	completions    *TextCompletionsInterface
	metadata       map[string]interface{}
	model          string
	clock          streamClock
	hideUsageChunk bool

	mu                sync.Mutex
	closed            bool
	usage             openai.CompletionUsage
	finishReason      string
	systemFingerprint string
	servedModel       string
	params            openai.CompletionNewParams
	reply             *prompt.ReplyCollector
}

func (s *TextCompletionStream) Next() bool {
	for s.stream.Next() {
		chunk := s.stream.Current()
		s.observe(chunk)
		if !s.isHiddenUsageChunk(chunk) {
			return true
		}
	}
	s.markExhausted()
	return false
}

func (s *TextCompletionStream) markExhausted() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.clock.markExhausted()
}

func (s *TextCompletionStream) Current() openai.Completion {
	return s.stream.Current()
}

func (s *TextCompletionStream) Err() error {
	return s.stream.Err()
}

func (s *TextCompletionStream) isHiddenUsageChunk(chunk openai.Completion) bool {
	return s.hideUsageChunk && len(chunk.Choices) == 0 && chunk.JSON.Usage.Valid()
}

func (s *TextCompletionStream) observe(chunk openai.Completion) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.clock.observe(textChunkHasOutput(chunk), textChoiceProgress(chunk))
	if chunk.JSON.Usage.Valid() {
		s.usage = chunk.Usage
	}
	if reason := textFinishReason(chunk); reason != "" {
		s.finishReason = reason
	}
	if chunk.SystemFingerprint != "" {
		s.systemFingerprint = chunk.SystemFingerprint
	}
	if s.servedModel == "" {
		s.servedModel = chunk.Model
	}
	s.reply.Add(textCompletionReply(chunk))
	if textCompletionFinished(chunk) {
		s.reply.End()
	}
}

func (s *TextCompletionStream) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return nil
	}
	s.closed = true

	err := s.stream.Close()
	chat := s.completions.chat
	provider := chat.provider

	payload := completionUsagePayload(provider.chatRecordModel(s.model, s.servedModel), provider, s.usage, s.finishReason, s.systemFingerprint).
		WithTiming(s.clock.start, s.clock.duration()).
		WithStreaming(true, s.clock.timeToFirstToken(), s.clock.firstToken).
		Build()
	applyStreamOutcome(payload, s.stream.Err(), s.metadata, func() metering.ChatCapture { return textCompletionCapture(s.params, "").WithStreamedReply(s.reply) })
	chat.parent.metering.Send(payload)
	return err
}
