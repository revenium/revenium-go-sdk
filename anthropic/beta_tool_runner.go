package anthropic

import (
	"context"
	"iter"
	"slices"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// BetaToolRunner wraps the upstream runner, whose turns call the unwrapped beta
// message service. Every method the upstream runner has is available; the ones
// that advance the conversation are redefined so each turn is gated and recorded.
type BetaToolRunner struct {
	*anthropic.BetaToolRunner
	gate turnGate
	sent *sentRequest
}

func (b *BetaMessagesInterface) NewToolRunner(tools []anthropic.BetaTool, params anthropic.BetaToolRunnerParams, opts ...option.RequestOption) *BetaToolRunner {
	sent := &sentRequest{}
	gate := newTurnGate(b)
	return &BetaToolRunner{
		BetaToolRunner: b.client.Beta.Messages.NewToolRunner(tools, params, withOption(gate.watching(opts), sent.middleware())...),
		gate:           gate,
		sent:           sent,
	}
}

func (r *BetaToolRunner) NextMessage(ctx context.Context) (*anthropic.BetaMessage, error) {
	if err := r.gate.admit(ctx, r, r.Params.Model, r.Params.MaxIterations); err != nil {
		return nil, err
	}
	turn := r.gate.startTurn(ctx, r.Params.BetaMessageNewParams)
	r.sent.clear()
	turn.capture = r.sent.capture

	message, err := r.BetaToolRunner.NextMessage(ctx)
	switch {
	case message != nil:
		turn.record(message, nil)
	case err != nil && r.gate.requestSent():
		turn.recordFailure(nil, err)
	}
	return message, err
}

func (r *BetaToolRunner) RunToCompletion(ctx context.Context) (*anthropic.BetaMessage, error) {
	for {
		message, err := r.NextMessage(ctx)
		if err != nil {
			return nil, err
		}
		if message == nil {
			return r.LastMessage(), nil
		}
	}
}

func (r *BetaToolRunner) All(ctx context.Context) iter.Seq2[*anthropic.BetaMessage, error] {
	return func(yield func(*anthropic.BetaMessage, error) bool) {
		for {
			message, err := r.NextMessage(ctx)
			if message == nil {
				if err != nil {
					yield(nil, err)
				}
				return
			}
			if !yield(message, nil) {
				return
			}
		}
	}
}

func (r *BetaToolRunner) Err() error {
	return r.gate.errOr(r.BetaToolRunner.Err())
}

type BetaToolRunnerStreaming struct {
	*anthropic.BetaToolRunnerStreaming
	gate turnGate
	sent *sentRequest
}

func (b *BetaMessagesInterface) NewToolRunnerStreaming(tools []anthropic.BetaTool, params anthropic.BetaToolRunnerParams, opts ...option.RequestOption) *BetaToolRunnerStreaming {
	sent := &sentRequest{}
	gate := newTurnGate(b)
	return &BetaToolRunnerStreaming{
		BetaToolRunnerStreaming: b.client.Beta.Messages.NewToolRunnerStreaming(tools, params, withOption(gate.watching(opts), sent.middleware())...),
		gate:                    gate,
		sent:                    sent,
	}
}

func (r *BetaToolRunnerStreaming) NextStreaming(ctx context.Context) iter.Seq2[anthropic.BetaRawMessageStreamEventUnion, error] {
	return func(yield func(anthropic.BetaRawMessageStreamEventUnion, error) bool) {
		if err := r.gate.admit(ctx, r, r.Params.Model, r.Params.MaxIterations); err != nil {
			yield(anthropic.BetaRawMessageStreamEventUnion{}, err)
			return
		}
		turn := r.gate.startTurn(ctx, r.Params.BetaMessageNewParams)
		r.sent.clear()
		turn.capture = r.sent.capture
		var observer betaStreamObserver
		var streamErr error
		defer func() {
			if streamErr != nil && r.gate.requestSent() {
				turn.recordFailure(&observer, streamErr)
				return
			}
			observer.recordInto(turn)
		}()

		for event, err := range r.BetaToolRunnerStreaming.NextStreaming(ctx) {
			if err == nil {
				observer.observe(event)
			} else {
				streamErr = err
				if observer.err == nil {
					observer.err = err
				}
			}
			if !yield(event, err) {
				return
			}
		}
		observer.progress.exhausted = true
	}
}

func (r *BetaToolRunnerStreaming) AllStreaming(ctx context.Context) iter.Seq2[iter.Seq2[anthropic.BetaRawMessageStreamEventUnion, error], error] {
	return func(yield func(iter.Seq2[anthropic.BetaRawMessageStreamEventUnion, error], error) bool) {
		for !r.IsCompleted() {
			if !yield(r.NextStreaming(ctx), nil) {
				return
			}
			if r.gate.blocked != nil {
				yield(nil, r.gate.blocked)
				return
			}
		}
	}
}

func (r *BetaToolRunnerStreaming) Err() error {
	return r.gate.errOr(r.BetaToolRunnerStreaming.Err())
}

type runnerProgress interface {
	IsCompleted() bool
	IterationCount() int
	LastMessage() *anthropic.BetaMessage
}

type turnGate struct {
	messages *BetaMessagesInterface
	blocked  error
	requests *requestSeen
}

func newTurnGate(messages *BetaMessagesInterface) turnGate {
	return turnGate{messages: messages, requests: &requestSeen{}}
}

func (g *turnGate) watching(opts []option.RequestOption) []option.RequestOption {
	return append(slices.Clip(opts), g.requests.option())
}

func (g *turnGate) startTurn(ctx context.Context, params anthropic.BetaMessageNewParams) *betaTurn {
	g.requests.sent.Store(false)
	return g.messages.startTurn(ctx, params)
}

func (g *turnGate) requestSent() bool {
	return g.requests.sent.Load()
}

func (g *turnGate) admit(ctx context.Context, progress runnerProgress, model anthropic.Model, maxIterations int) error {
	g.blocked = nil
	if runnerWillCallModel(progress, maxIterations) {
		g.blocked = g.messages.checkCostControls(ctx, model)
	}
	return g.blocked
}

func (g *turnGate) errOr(runnerErr error) error {
	if g.blocked != nil {
		return g.blocked
	}
	return runnerErr
}

// runnerWillCallModel predicts whether the upstream runner's next step sends a
// request, so a finished conversation is not reported as blocked. It follows the
// runner's stop-reason handling in anthropic-sdk-go betatoolrunner.go.
func runnerWillCallModel(progress runnerProgress, maxIterations int) bool {
	if progress.IsCompleted() || (maxIterations > 0 && progress.IterationCount() >= maxIterations) {
		return false
	}
	last := progress.LastMessage()
	if last == nil {
		return true
	}
	switch last.StopReason {
	case anthropic.BetaStopReasonPauseTurn, anthropic.BetaStopReasonCompaction:
		return true
	case anthropic.BetaStopReasonToolUse:
		return slices.ContainsFunc(last.Content, isToolUseBlock)
	default:
		return false
	}
}

func isToolUseBlock(block anthropic.BetaContentBlockUnion) bool {
	return block.Type == "tool_use"
}

// withOption adds option to a copy of opts, so a runner never writes into the caller's slice.
func withOption(opts []option.RequestOption, extra option.RequestOption) []option.RequestOption {
	return append(slices.Clip(opts), extra)
}
