package google

import (
	"context"
	"io"
	"iter"
	"slices"

	"google.golang.org/genai"
)

type ChatsInterface struct {
	models *ModelsInterface
}

func (r *ReveniumGoogle) Chats() *ChatsInterface {
	return &ChatsInterface{models: r.Models()}
}

// Chat mirrors genai.Chat, whose turns call the unwrapped Models service and so
// cannot be metered; each turn here goes through ModelsInterface instead.
type Chat struct {
	models               *ModelsInterface
	model                string
	config               *genai.GenerateContentConfig
	comprehensiveHistory []*genai.Content
	curatedHistory       []*genai.Content
}

func (c *ChatsInterface) Create(
	ctx context.Context,
	model string,
	config *genai.GenerateContentConfig,
	history []*genai.Content,
) (*Chat, error) {
	seed, err := c.models.client.Chats.Create(ctx, model, config, history)
	if err != nil {
		return nil, err
	}
	return &Chat{
		models:               c.models,
		model:                model,
		config:               config,
		comprehensiveHistory: seed.History(false),
		curatedHistory:       seed.History(true),
	}, nil
}

func (c *Chat) History(curated bool) []*genai.Content {
	if curated {
		return c.curatedHistory
	}
	return c.comprehensiveHistory
}

func (c *Chat) SendMessage(ctx context.Context, parts ...genai.Part) (*genai.GenerateContentResponse, error) {
	return c.Send(ctx, partPointers(parts)...)
}

func (c *Chat) Send(ctx context.Context, parts ...*genai.Part) (*genai.GenerateContentResponse, error) {
	input := userTurn(parts)
	resp, err := c.models.GenerateContent(ctx, c.model, c.contentsWith(input), c.config)
	if err != nil {
		return nil, err
	}
	c.recordTurn(input, firstCandidateContents(resp), isValidResponse(resp))
	return resp, nil
}

func (c *Chat) SendMessageStream(ctx context.Context, parts ...genai.Part) iter.Seq2[*genai.GenerateContentResponse, error] {
	return c.SendStream(ctx, partPointers(parts)...)
}

func (c *Chat) SendStream(ctx context.Context, parts ...*genai.Part) iter.Seq2[*genai.GenerateContentResponse, error] {
	input := userTurn(parts)
	stream := c.models.GenerateContentStream(ctx, c.model, c.contentsWith(input), c.config)

	return func(yield func(*genai.GenerateContentResponse, error) bool) {
		var outputs []*genai.Content
		valid := true
		var finishReason genai.FinishReason
		for chunk, err := range stream {
			if err == io.EOF {
				break
			}
			if err != nil {
				yield(nil, err)
				return
			}
			valid = valid && (isUsageOnly(chunk) || isValidResponse(chunk))
			outputs = append(outputs, firstCandidateContents(chunk)...)
			if reason := ExtractFinishReason(chunk); reason != "" {
				finishReason = reason
			}
			if !yield(chunk, nil) {
				return
			}
		}
		c.recordTurn(input, outputs, valid && isFinished(finishReason))
	}
}

func (c *Chat) contentsWith(input *genai.Content) []*genai.Content {
	return append(slices.Clone(c.curatedHistory), input)
}

func (c *Chat) recordTurn(input *genai.Content, outputs []*genai.Content, valid bool) {
	if len(outputs) == 0 {
		outputs = []*genai.Content{{Role: genai.RoleModel, Parts: []*genai.Part{}}}
	}
	c.comprehensiveHistory = append(append(c.comprehensiveHistory, input), outputs...)
	if valid {
		c.curatedHistory = append(append(c.curatedHistory, input), outputs...)
	}
}

func isFinished(reason genai.FinishReason) bool {
	return reason != "" && reason != genai.FinishReasonUnspecified
}

func userTurn(parts []*genai.Part) *genai.Content {
	return &genai.Content{Parts: parts, Role: genai.RoleUser}
}

func partPointers(parts []genai.Part) []*genai.Part {
	pointers := make([]*genai.Part, len(parts))
	for i := range parts {
		pointers[i] = &parts[i]
	}
	return pointers
}

func firstCandidateContents(resp *genai.GenerateContentResponse) []*genai.Content {
	if resp == nil || len(resp.Candidates) == 0 || resp.Candidates[0].Content == nil {
		return nil
	}
	return []*genai.Content{resp.Candidates[0].Content}
}

func isUsageOnly(chunk *genai.GenerateContentResponse) bool {
	return len(chunk.Candidates) == 0 && chunk.UsageMetadata != nil
}

func isValidResponse(resp *genai.GenerateContentResponse) bool {
	contents := firstCandidateContents(resp)
	return len(contents) == 1 && isValidContent(contents[0])
}

func isValidContent(content *genai.Content) bool {
	if content == nil || len(content.Parts) == 0 {
		return false
	}
	return !slices.ContainsFunc(content.Parts, isEmptyPart)
}

func isEmptyPart(part *genai.Part) bool {
	if part == nil {
		return true
	}
	return part.Text == "" &&
		part.InlineData == nil &&
		part.FileData == nil &&
		part.FunctionCall == nil &&
		part.FunctionResponse == nil &&
		part.ExecutableCode == nil &&
		part.CodeExecutionResult == nil
}
