package google

import (
	"strings"

	"google.golang.org/genai"

	"github.com/revenium/revenium-go-sdk/core/metering"
	"github.com/revenium/revenium-go-sdk/core/prompt"
)

func contentsCapture(contents []*genai.Content, config *genai.GenerateContentConfig, reply string) metering.ChatCapture {
	var system string
	if config != nil {
		system = strings.Join(partTexts(config.SystemInstruction), "\n")
	}
	return metering.ChatCapture{SystemPrompt: system, Messages: capturedContents(contents), Output: reply}
}

// capturedContents keeps each turn's text and stands a placeholder in for inline data,
// files and function calls, so no binary data is captured. Thoughts replayed from earlier
// turns are left out, as they are from the captured reply.
func capturedContents(contents []*genai.Content) []interface{} {
	captured := make([]interface{}, 0, len(contents))
	for _, content := range contents {
		if content == nil {
			continue
		}
		role := content.Role
		if role == "" {
			role = genai.RoleUser
		}
		parts := make([]interface{}, 0, len(content.Parts))
		for _, part := range content.Parts {
			if part != nil && !part.Thought {
				parts = append(parts, capturedPart(part))
			}
		}
		captured = append(captured, map[string]interface{}{"role": role, "parts": parts})
	}
	return captured
}

func capturedPart(part *genai.Part) map[string]interface{} {
	switch {
	case part.Text != "":
		return map[string]interface{}{"text": part.Text}
	case part.InlineData != nil:
		return map[string]interface{}{"inline_data": prompt.BinaryPlaceholder}
	case part.FileData != nil:
		return map[string]interface{}{"file_data": "[FILE_DATA]"}
	case part.FunctionCall != nil:
		return map[string]interface{}{"function_call": "[FUNCTION_CALL]"}
	case part.FunctionResponse != nil:
		return map[string]interface{}{"function_response": "[FUNCTION_RESPONSE]"}
	default:
		return map[string]interface{}{"unknown": "[UNKNOWN_PART_TYPE]"}
	}
}

// responseReply is the answer text of the candidate with index 0, without thoughts; with
// several candidates each streams in its own chunks, and the captured reply is the first.
func responseReply(resp *genai.GenerateContentResponse, separator string) string {
	if resp == nil {
		return ""
	}
	for _, candidate := range resp.Candidates {
		if candidate != nil && candidate.Index == 0 {
			return strings.Join(partTexts(candidate.Content), separator)
		}
	}
	return ""
}

func partTexts(content *genai.Content) []string {
	if content == nil {
		return nil
	}
	texts := make([]string, 0, len(content.Parts))
	for _, part := range content.Parts {
		if part != nil && part.Text != "" && !part.Thought {
			texts = append(texts, part.Text)
		}
	}
	return texts
}

// firstCandidateFinished reports whether the candidate with index 0 ended in this chunk, so
// its reply is whole even when the caller stops reading there.
func firstCandidateFinished(resp *genai.GenerateContentResponse) bool {
	for _, candidate := range resp.Candidates {
		if candidate != nil && candidate.Index == 0 {
			return candidate.FinishReason != ""
		}
	}
	return false
}
