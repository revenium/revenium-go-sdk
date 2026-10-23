package metering

import (
	"slices"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/revenium/revenium-go-sdk/core/prompt"
)

const systemRole = "system"

// ChatCapture is what prompt capture records for one chat call: the system
// prompt, the other input messages as sent to the provider, and the reply text.
// OutputCut says the reply was already cut before it got here.
type ChatCapture struct {
	SystemPrompt string
	Messages     []interface{}
	Output       string
	OutputCut    bool
}

// WithStreamedReply sets the reply to what collector gathered, cut if the collector dropped
// any of it or the stream was stopped before its end.
func (c ChatCapture) WithStreamedReply(collector *prompt.ReplyCollector) ChatCapture {
	c.Output, c.OutputCut = collector.Text(), !collector.Complete()
	return c
}

// ApplyChatCapture stores a chat call's captured prompt and reply on payload
// when prompt capture is on for metadata; capture is only called then. Each of
// systemPrompt, inputMessages and outputResponse has credentials redacted and is
// cut to REVENIUM_MAX_PROMPT_SIZE, and promptsTruncated says whether a stored
// field was cut. A system prompt found in the request replaces one given in usage
// metadata. Call it after ApplyMetadata. A failure to capture leaves the record
// without the prompt fields rather than losing the record.
func ApplyChatCapture(payload *MeteringPayload, metadata map[string]interface{}, capture func() ChatCapture) {
	if payload == nil || capture == nil || !prompt.ShouldCapturePrompts(metadata) {
		return
	}
	captured, ok := boundedChatCapture(capture)
	if !ok {
		return
	}
	systemCut := payload.PromptsTruncated
	if captured.systemPrompt != "" {
		payload.SystemPrompt, systemCut = captured.systemPrompt, captured.systemCut
	}
	if captured.inputMessages != "" {
		payload.InputMessages = captured.inputMessages
	}
	if captured.outputResponse != "" {
		payload.OutputResponse = captured.outputResponse
	}
	payload.PromptsTruncated = systemCut || captured.messagesCut || captured.outputCut
}

type boundedChat struct {
	systemPrompt   string
	inputMessages  string
	outputResponse string
	systemCut      bool
	messagesCut    bool
	outputCut      bool
}

func boundedChatCapture(capture func() ChatCapture) (bounded boundedChat, ok bool) {
	defer func() {
		if recovered := recover(); recovered != nil {
			core.Warn("prompt capture skipped for this call: %v", recovered)
			bounded, ok = boundedChat{}, false
		}
	}()

	chat := capture()
	maxSize := prompt.GetMaxPromptSize()
	if !core.IsBlank(chat.SystemPrompt) {
		bounded.systemPrompt, bounded.systemCut = boundedCapture(chat.SystemPrompt, maxSize)
	}
	if !core.IsBlank(chat.Output) {
		bounded.outputResponse, bounded.outputCut = withinLimit(sanitizedReply(chat), maxSize)
		if chat.OutputCut && !bounded.outputCut {
			bounded.outputResponse = prompt.MarkTruncated(bounded.outputResponse, maxSize)
		}
	}
	bounded.outputCut = bounded.outputCut || chat.OutputCut
	if len(chat.Messages) > 0 {
		masked, _ := prompt.SanitizeCredentialsIn(chat.Messages).([]interface{})
		text, cut, err := prompt.SerializeMessagesWithinLimit(masked, maxSize)
		if err != nil {
			core.Warn("prompt capture could not encode the input messages: %v", err)
		}
		bounded.inputMessages, bounded.messagesCut = text, cut
	}
	return bounded, true
}

func sanitizedReply(chat ChatCapture) string {
	if chat.OutputCut {
		return prompt.SanitizeCutText(chat.Output)
	}
	return prompt.SanitizeCredentials(chat.Output)
}

// ChatMessages returns messages, any JSON-encodable list such as a provider
// SDK's typed message params, as the plain JSON values sent to the provider, with
// inline file data replaced by a placeholder.
func ChatMessages(messages interface{}) []interface{} {
	decoded, err := prompt.DecodeJSONValues(messages)
	if err != nil {
		core.Warn("prompt capture could not read the input messages: %v", err)
		return nil
	}
	list, _ := prompt.WithoutInlineData(decoded).([]interface{})
	return list
}

// OpenAIChatCapture is the capture of a call whose messages have the OpenAI
// chat shape, given as any JSON-encodable list: its first system message is the
// system prompt, and later system messages are left out, as the Python SDK does.
func OpenAIChatCapture(messages interface{}, reply string) ChatCapture {
	systemPrompt, others := SplitSystemMessage(ChatMessages(messages), false, systemRole)
	return ChatCapture{SystemPrompt: systemPrompt, Messages: others, Output: reply}
}

// SplitSystemMessage takes the first message with one of roles out as the system prompt
// and returns the other messages. keepLater says whether later messages with those roles
// stay among them; Python's chat extractor drops them and its Responses extractor keeps them.
func SplitSystemMessage(messages []interface{}, keepLater bool, roles ...string) (string, []interface{}) {
	systemPrompt := ""
	foundSystem := false
	others := make([]interface{}, 0, len(messages))
	for _, message := range messages {
		fields, isObject := message.(map[string]interface{})
		role, _ := fields["role"].(string)
		if !isObject || !slices.Contains(roles, role) {
			others = append(others, message)
			continue
		}
		if !foundSystem {
			systemPrompt, foundSystem = prompt.ContentText(fields["content"]), true
		} else if keepLater {
			others = append(others, message)
		}
	}
	return systemPrompt, others
}

// ReplyCollectorFor returns a collector for a streamed reply when prompt
// capture is on for metadata, and nil, which ignores what it is given, otherwise.
func ReplyCollectorFor(metadata map[string]interface{}) *prompt.ReplyCollector {
	if !prompt.ShouldCapturePrompts(metadata) {
		return nil
	}
	return prompt.NewReplyCollector()
}
