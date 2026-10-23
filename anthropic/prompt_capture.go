package anthropic

import (
	"encoding/json"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/revenium/revenium-go-sdk/core/metering"
	"github.com/revenium/revenium-go-sdk/core/prompt"
)

const textBlockType = "text"

func messageCapture(params anthropic.MessageNewParams, reply string) metering.ChatCapture {
	system := make([]string, len(params.System))
	for i, block := range params.System {
		system[i] = block.Text
	}
	return turnCapture(system, params.Messages, reply)
}

func betaMessageCapture(params anthropic.BetaMessageNewParams, reply string) metering.ChatCapture {
	system := make([]string, len(params.System))
	for i, block := range params.System {
		system[i] = block.Text
	}
	return turnCapture(system, params.Messages, reply)
}

func turnCapture(system []string, messages interface{}, reply string) metering.ChatCapture {
	return metering.ChatCapture{
		SystemPrompt: strings.Join(system, "\n"),
		Messages:     metering.ChatMessages(messages),
		Output:       reply,
	}
}

// replyBlock is one content block of a reply: its text, or for any other kind
// of block (tool use, thinking) the block as it would be sent back.
type replyBlock struct {
	blockType string
	text      string
	param     interface{}
	partial   map[string]interface{}
}

func messageReply(content []anthropic.ContentBlockUnion) string {
	blocks := make([]replyBlock, len(content))
	for i, block := range content {
		blocks[i] = replyBlock{blockType: block.Type, text: block.Text, param: block.ToParam(), partial: partialToolUse(block.Type, block.ID, block.Name, block.Input)}
	}
	return replyText(blocks, false)
}

func betaMessageReply(content []anthropic.BetaContentBlockUnion, cut bool) string {
	blocks := make([]replyBlock, len(content))
	for i, block := range content {
		blocks[i] = replyBlock{blockType: block.Type, text: block.Text, param: block.ToParam(), partial: partialToolUse(block.Type, block.ID, block.Name, block.Input)}
	}
	return replyText(blocks, cut)
}

// partialToolUse is a tool call whose streamed input stopped partway: the input is not valid
// JSON, so the block cannot be encoded as sent, and is kept with its input as text. It is
// nil for every other block.
func partialToolUse(blockType, id, name string, input json.RawMessage) map[string]interface{} {
	if len(input) == 0 || json.Valid(input) {
		return nil
	}
	return map[string]interface{}{"type": blockType, "id": id, "name": name, "input": string(input)}
}

// replyText is the one rule for a reply as captured text, streamed or not: text blocks as
// text, every other block as the JSON it would be sent back as, each masked on its own,
// joined by newlines. A cut reply masks a credential cut at the end of any of its strings.
func replyText(blocks []replyBlock, cut bool) string {
	sanitize := prompt.SanitizeCredentials
	if cut {
		sanitize = prompt.SanitizeCutText
	}
	parts := make([]string, 0, len(blocks))
	for _, block := range blocks {
		if block.blockType == textBlockType {
			parts = append(parts, block.text)
			continue
		}
		source := block.param
		if block.partial != nil {
			source = block.partial
		}
		if encoded, err := json.Marshal(source); err == nil {
			parts = append(parts, sanitize(string(encoded)))
		}
	}
	return strings.Join(parts, "\n")
}
