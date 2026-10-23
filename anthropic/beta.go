package anthropic

import (
	"context"
	"sync"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/anthropics/anthropic-sdk-go/packages/ssestream"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/revenium/revenium-go-sdk/core/enforcement"
	"github.com/revenium/revenium-go-sdk/core/metering"
)

const anthropicAPIProvider = "Anthropic"

type BetaMessagesInterface struct {
	client anthropic.Client
	parent *ReveniumAnthropic
}

func (r *ReveniumAnthropic) BetaMessages() *BetaMessagesInterface {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return &BetaMessagesInterface{client: r.client, parent: r}
}

func (b *BetaMessagesInterface) CreateMessage(ctx context.Context, params anthropic.BetaMessageNewParams, opts ...option.RequestOption) (*anthropic.BetaMessage, error) {
	if err := b.checkCostControls(ctx, params.Model); err != nil {
		return nil, err
	}
	turn := b.startTurn(ctx, params)

	resp, err := b.client.Beta.Messages.New(ctx, params, opts...)
	if err != nil {
		turn.recordFailure(nil, err)
		return nil, err
	}

	turn.record(resp, nil)
	return resp, nil
}

func (b *BetaMessagesInterface) CreateMessageStream(ctx context.Context, params anthropic.BetaMessageNewParams, opts ...option.RequestOption) (*BetaMessageStream, error) {
	if err := b.checkCostControls(ctx, params.Model); err != nil {
		return nil, err
	}
	turn := b.startTurn(ctx, params)

	return &BetaMessageStream{
		stream: b.client.Beta.Messages.NewStreaming(ctx, params, opts...),
		turn:   turn,
	}, nil
}

func (b *BetaMessagesInterface) checkCostControls(ctx context.Context, model anthropic.Model) error {
	if err := b.parent.enforcer.Check(enforcement.NewEvalContext(ctx, string(model), anthropicAPIProvider).ServedModelMayBeDated()); err != nil {
		core.Debug("Beta message blocked by enforcement: %v", err)
		return err
	}
	return nil
}

func (b *BetaMessagesInterface) startTurn(ctx context.Context, params anthropic.BetaMessageNewParams) *betaTurn {
	return &betaTurn{
		model:        string(params.Model),
		messages:     b,
		metadata:     core.GetUsageMetadata(ctx),
		hasVision:    detectBetaVisionContent(params.Messages),
		advisorModel: advisorToolModel(params.Tools),
		start:        time.Now(),
		capture: func(reply string) metering.ChatCapture {
			return betaMessageCapture(params, reply)
		},
	}
}

type betaTurn struct {
	model        string
	messages     *BetaMessagesInterface
	metadata     map[string]interface{}
	hasVision    bool
	advisorModel string
	start        time.Time
	capture      func(reply string) metering.ChatCapture
}

func (t *betaTurn) record(resp *anthropic.BetaMessage, stream *betaStreamObserver) {
	timing := turnTiming{start: t.start, duration: time.Since(t.start)}
	if stream != nil {
		timing.streamed = true
		timing.firstTokenTime = stream.firstTokenTime
	}
	usage := betaMessageTurnUsage(resp)
	payload := buildTurnPayload(usage, timing, anthropicAPIProvider, t.hasVision, t.metadata)
	if stream != nil && stream.progress.abandonedBeforeStop() {
		payload.StopReason = string(core.StopReasonCancelled)
	}
	if stream == nil || stream.err == nil {
		metering.ApplyChatCapture(payload, t.metadata, func() metering.ChatCapture {
			cut := stream != nil && !stream.progress.stopEventSeen
			capture := t.capture(betaMessageReply(resp.Content, cut))
			capture.OutputCut = cut
			return capture
		})
	}
	sendTurnRecords(t.messages.parent.metering, payload, usage.passes, t.advisorModel)
}

// recordFailure records a turn whose request failed, keeping the usage a
// stream reported before it broke off.
func (t *betaTurn) recordFailure(stream *betaStreamObserver, err error) {
	usage := turnUsage{model: t.model}
	timing := turnTiming{start: t.start, duration: time.Since(t.start)}
	if stream != nil {
		timing.streamed = true
		timing.firstTokenTime = stream.firstTokenTime
		if stream.started {
			usage = betaMessageTurnUsage(&stream.message)
		}
	}
	payload := buildFailedTurnPayload(usage, timing, anthropicAPIProvider, t.hasVision, t.metadata, err)
	sendTurnRecords(t.messages.parent.metering, payload, usage.passes, t.advisorModel)
}

type betaStreamObserver struct {
	message        anthropic.BetaMessage
	started        bool
	firstTokenTime *time.Time
	err            error
	progress       streamProgress
}

func (o *betaStreamObserver) observe(event anthropic.BetaRawMessageStreamEventUnion) {
	switch event.Type {
	case "message_start":
		o.started = true
	case "content_block_delta":
		if o.firstTokenTime == nil {
			now := time.Now()
			o.firstTokenTime = &now
		}
	case "message_delta":
		o.replaceStaleCacheWriteSplit(event.Usage)
	case "message_stop":
		o.progress.stopEventSeen = true
	}
	if err := o.message.Accumulate(event); err != nil {
		core.Debug("Beta stream event could not be accumulated: %v", err)
	}
}

// Accumulate takes a delta's cache-write total but keeps message_start's per-TTL split.
func (o *betaStreamObserver) replaceStaleCacheWriteSplit(usage anthropic.BetaMessageDeltaUsage) {
	if usage.JSON.CacheCreationInputTokens.Valid() && usage.CacheCreationInputTokens != o.message.Usage.CacheCreationInputTokens {
		o.message.Usage.CacheCreation = betaCacheCreationInUsage(usage.RawJSON())
	}
}

func (o *betaStreamObserver) recordInto(turn *betaTurn) {
	if o.started {
		turn.record(&o.message, o)
	}
}

type BetaMessageStream struct {
	stream   *ssestream.Stream[anthropic.BetaRawMessageStreamEventUnion]
	turn     *betaTurn
	observer betaStreamObserver
	mu       sync.Mutex
	closed   bool
}

func (s *BetaMessageStream) Next() bool {
	if !s.stream.Next() {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.observer.progress.exhausted = true
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.observer.observe(s.stream.Current())
	return true
}

func (s *BetaMessageStream) Current() anthropic.BetaRawMessageStreamEventUnion {
	return s.stream.Current()
}

func (s *BetaMessageStream) Err() error {
	return s.stream.Err()
}

func (s *BetaMessageStream) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return nil
	}
	s.closed = true

	s.observer.err = s.stream.Err()
	err := s.stream.Close()
	if streamErr := s.stream.Err(); streamErr != nil {
		s.turn.recordFailure(&s.observer, streamErr)
		return err
	}
	s.observer.recordInto(s.turn)
	return err
}

func detectBetaVisionContent(messages []anthropic.BetaMessageParam) bool {
	for _, msg := range messages {
		for _, block := range msg.Content {
			if block.OfImage != nil || betaToolResultCarriesImage(block.OfToolResult) {
				return true
			}
		}
	}
	return false
}

func betaToolResultCarriesImage(result *anthropic.BetaToolResultBlockParam) bool {
	if result == nil {
		return false
	}
	for _, part := range result.Content {
		if part.OfImage != nil {
			return true
		}
	}
	return false
}
