package anthropic

import (
	"encoding/json"
	"sort"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/aws/aws-sdk-go-v2/aws"
	brtypes "github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"

	"github.com/revenium/revenium-go-sdk/core/prompt"
)

const (
	toolUseBlockType   = "tool_use"
	serverToolType     = "server_tool_use"
	thinkingBlockType  = "thinking"
	textDeltaType      = "text_delta"
	inputJSONDeltaType = "input_json_delta"
	thinkingDeltaType  = "thinking_delta"
	signatureDeltaType = "signature_delta"
)

// replyCapturer is a Bedrock stream that builds the streamed reply itself, because its
// events never reach the outer wrapper's typed event handling.
type replyCapturer interface {
	captureReply()
	capturedReply() (text string, cut bool)
}

type streamedBlock struct {
	kind      string
	id        string
	name      string
	start     string
	body      strings.Builder
	signature strings.Builder
}

// streamedReply gathers a streamed reply's text, tool calls, thinking and other blocks for
// prompt capture. Everything it keeps, blocks, their ids and names, bodies, signatures and
// start payloads, counts against one prompt.ReplyBudget, and once the budget is spent it
// opens no new blocks, so a long reply cannot grow memory without bound.
type streamedReply struct {
	blocks   map[int64]*streamedBlock
	budget   *prompt.ReplyBudget
	complete bool
}

func newStreamedReply() *streamedReply {
	return &streamedReply{blocks: map[int64]*streamedBlock{}, budget: prompt.NewReplyBudget()}
}

// block returns the block at index, opening it only while the budget has room. Every block
// costs at least one character, so a stream of empty blocks is bounded too.
func (r *streamedReply) block(index int64) *streamedBlock {
	if block := r.blocks[index]; block != nil {
		return block
	}
	if !r.budget.Charge(1) {
		return nil
	}
	r.blocks[index] = &streamedBlock{kind: textBlockType}
	return r.blocks[index]
}

func (r *streamedReply) start(index int64, kind, id, name, raw string) *streamedBlock {
	block := r.block(index)
	if block == nil {
		return nil
	}
	block.kind = kind
	if r.budget.Keep(id + name) {
		block.id, block.name = id, name
	}
	switch kind {
	case textBlockType, toolUseBlockType, serverToolType, thinkingBlockType:
	default:
		if r.budget.Keep(raw) {
			block.start = raw
		}
	}
	return block
}

func (r *streamedReply) appendBody(index int64, text string) {
	if block := r.block(index); block != nil {
		r.budget.Write(&block.body, text)
	}
}

func (r *streamedReply) appendSignature(index int64, text string) {
	if block := r.block(index); block != nil {
		r.budget.Write(&block.signature, text)
	}
}

func (r *streamedReply) observe(event anthropic.MessageStreamEventUnion) {
	switch event.Type {
	case "message_stop":
		r.complete = true
	case "content_block_start":
		started := event.ContentBlock
		if r.start(event.Index, started.Type, started.ID, started.Name, started.RawJSON()) != nil {
			r.appendBody(event.Index, started.Text)
		}
	case "content_block_delta":
		switch event.Delta.Type {
		case textDeltaType:
			r.appendBody(event.Index, event.Delta.Text)
		case inputJSONDeltaType:
			r.appendBody(event.Index, event.Delta.PartialJSON)
		case thinkingDeltaType:
			r.appendBody(event.Index, event.Delta.Thinking)
		case signatureDeltaType:
			r.appendSignature(event.Index, event.Delta.Signature)
		}
	}
}

func (r *streamedReply) observeConverse(event brtypes.ConverseStreamOutput) {
	switch ev := event.(type) {
	case *brtypes.ConverseStreamOutputMemberMessageStop:
		r.complete = true
	case *brtypes.ConverseStreamOutputMemberContentBlockStart:
		if start, ok := ev.Value.Start.(*brtypes.ContentBlockStartMemberToolUse); ok {
			r.start(int64(aws.ToInt32(ev.Value.ContentBlockIndex)), toolUseBlockType, aws.ToString(start.Value.ToolUseId), aws.ToString(start.Value.Name), "")
		}
	case *brtypes.ConverseStreamOutputMemberContentBlockDelta:
		index := int64(aws.ToInt32(ev.Value.ContentBlockIndex))
		switch delta := ev.Value.Delta.(type) {
		case *brtypes.ContentBlockDeltaMemberText:
			r.appendBody(index, delta.Value)
		case *brtypes.ContentBlockDeltaMemberToolUse:
			r.appendBody(index, aws.ToString(delta.Value.Input))
		case *brtypes.ContentBlockDeltaMemberReasoningContent:
			r.observeConverseReasoning(index, delta.Value)
		}
	}
}

func (r *streamedReply) observeConverseReasoning(index int64, delta brtypes.ReasoningContentBlockDelta) {
	block := r.block(index)
	if block == nil {
		return
	}
	switch reasoning := delta.(type) {
	case *brtypes.ReasoningContentBlockDeltaMemberText:
		block.kind = thinkingBlockType
		r.budget.Write(&block.body, reasoning.Value)
	case *brtypes.ReasoningContentBlockDeltaMemberSignature:
		block.kind = thinkingBlockType
		r.budget.Write(&block.signature, reasoning.Value)
	}
}

// text is the reply in messageReply's form. It is cut when the budget dropped part of it or
// the stream closed before its stop event, even when the transport closed cleanly.
func (r *streamedReply) text() (text string, cut bool) {
	indices := make([]int64, 0, len(r.blocks))
	for index := range r.blocks {
		indices = append(indices, index)
	}
	sort.Slice(indices, func(i, j int) bool { return indices[i] < indices[j] })
	blocks := make([]replyBlock, 0, len(indices))
	for _, index := range indices {
		if block, ok := r.blocks[index].replyBlock(); ok {
			blocks = append(blocks, block)
		}
	}
	cut = r.budget.Cut() || !r.complete
	return replyText(blocks, cut), cut
}

func (b *streamedBlock) replyBlock() (replyBlock, bool) {
	switch b.kind {
	case textBlockType:
		return replyBlock{blockType: b.kind, text: b.body.String()}, true
	case toolUseBlockType, serverToolType:
		return replyBlock{blockType: b.kind, param: map[string]interface{}{"type": b.kind, "id": b.id, "name": b.name, "input": streamedToolInput(b.body.String())}}, true
	case thinkingBlockType:
		return replyBlock{blockType: b.kind, param: map[string]interface{}{"type": b.kind, "thinking": b.body.String(), "signature": b.signature.String()}}, true
	}
	if b.start == "" {
		return replyBlock{}, false
	}
	return replyBlock{blockType: b.kind, param: json.RawMessage(b.start)}, true
}

// streamedToolInput keeps a tool call's arguments as JSON, or as text when the cut left them incomplete.
func streamedToolInput(raw string) interface{} {
	if raw == "" {
		return json.RawMessage(`{}`)
	}
	if json.Valid([]byte(raw)) {
		return json.RawMessage(raw)
	}
	return raw
}
