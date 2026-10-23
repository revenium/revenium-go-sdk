package openai

import (
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"
	"github.com/revenium/revenium-go-sdk/core/metering"
)

func chatCapture(params openai.ChatCompletionNewParams, reply string) metering.ChatCapture {
	return metering.OpenAIChatCapture(params.Messages, reply)
}

// chatReply, choiceZero, chunkReply and chunkFinished have copies in perplexity/middleware.go
// that must change with them: providers may not import each other.
func chatReply(resp *openai.ChatCompletion) string {
	if resp == nil || len(resp.Choices) == 0 {
		return ""
	}
	return resp.Choices[0].Message.Content
}

// choiceZero is the first choice's part of a streamed chunk; with n > 1 each choice
// streams in its own chunks, and the record describes the first choice, as when not streamed.
func choiceZero(chunk openai.ChatCompletionChunk) openai.ChatCompletionChunkChoice {
	for _, choice := range chunk.Choices {
		if choice.Index == 0 {
			return choice
		}
	}
	return openai.ChatCompletionChunkChoice{}
}

func chunkReply(chunk openai.ChatCompletionChunk) string {
	return choiceZero(chunk).Delta.Content
}

// chunkFinished reports whether the first choice ended in this chunk, so its reply is whole
// even when the caller stops reading there.
func chunkFinished(chunk openai.ChatCompletionChunk) bool {
	return choiceZero(chunk).FinishReason != ""
}

// responsesCapture takes the system prompt from instructions, else from the first system
// or developer input item, which is then left out of the messages; later ones stay, as
// in the Python SDK's Responses extractor.
func responsesCapture(params responses.ResponseNewParams, reply string) metering.ChatCapture {
	items := responsesInputItems(params.Input)
	if params.Instructions.Valid() {
		return metering.ChatCapture{SystemPrompt: params.Instructions.Value, Messages: items, Output: reply}
	}
	systemPrompt, others := metering.SplitSystemMessage(items, true, "system", "developer")
	return metering.ChatCapture{SystemPrompt: systemPrompt, Messages: others, Output: reply}
}

func responsesInputItems(input responses.ResponseNewParamsInputUnion) []interface{} {
	if input.OfString.Valid() {
		return []interface{}{userMessage(input.OfString.Value)}
	}
	if len(input.OfInputItemList) == 0 {
		return nil
	}
	return metering.ChatMessages(input.OfInputItemList)
}

func textCompletionCapture(params openai.CompletionNewParams, reply string) metering.ChatCapture {
	var messages []interface{}
	if params.Prompt.OfString.Valid() {
		messages = append(messages, userMessage(params.Prompt.OfString.Value))
	}
	for _, text := range params.Prompt.OfArrayOfStrings {
		messages = append(messages, userMessage(text))
	}
	return metering.ChatCapture{Messages: messages, Output: reply}
}

func textCompletionFinished(completion openai.Completion) bool {
	for _, choice := range completion.Choices {
		if choice.Index == 0 {
			return choice.FinishReason != ""
		}
	}
	return false
}

func textCompletionReply(completion openai.Completion) string {
	for _, choice := range completion.Choices {
		if choice.Index == 0 {
			return choice.Text
		}
	}
	return ""
}

func userMessage(content string) map[string]interface{} {
	return map[string]interface{}{"role": "user", "content": content}
}
