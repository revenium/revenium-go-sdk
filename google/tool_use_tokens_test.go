package google

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/genai"
)

const (
	geminiToolUseResponse         = `{"candidates":[{"content":{"parts":[{"text":"5117"}],"role":"model"},"finishReason":"STOP","index":0}],"usageMetadata":{"candidatesTokenCount":10,"promptTokenCount":20,"toolUsePromptTokenCount":500,"toolUsePromptTokensDetails":[{"modality":"TEXT","tokenCount":500}],"totalTokenCount":530},"modelVersion":"gemini-3.5-flash-lite"}`
	geminiToolUseThinkingResponse = `{"candidates":[{"content":{"parts":[{"text":"5117"}],"role":"model"},"finishReason":"STOP","index":0}],"usageMetadata":{"candidatesTokenCount":122,"promptTokenCount":22,"thoughtsTokenCount":40,"toolUsePromptTokenCount":143,"totalTokenCount":327},"modelVersion":"gemini-3.5-flash-lite"}`
)

func TestToolUsePromptTokensAreCountedAsInput(t *testing.T) {
	cases := []struct {
		name                            string
		response                        string
		input, output, reasoning, total float64
	}{
		{"code execution", geminiToolUseResponse, 520, 10, 0, 530},
		{"code execution with thinking", geminiToolUseThinkingResponse, 165, 162, 40, 327},
	}
	for _, provider := range streamProviders {
		for _, tc := range cases {
			t.Run(provider.String()+" "+tc.name+" non-streamed", func(t *testing.T) {
				c := newStubbedClient(t, provider, func(w http.ResponseWriter, r *http.Request) {
					writeJSON(w, http.StatusOK, tc.response)
				})

				_, err := c.client.Models().GenerateContent(context.Background(), "gemini-3.5-flash-lite", genai.Text("What is 7 * 731? Run code."), codeExecutionConfig())
				require.NoError(t, err)

				assertTokenCounts(t, singleRecord(t, c), tc.input, tc.output, tc.reasoning, tc.total)
			})
			t.Run(provider.String()+" "+tc.name+" streamed", func(t *testing.T) {
				c := newStubbedClient(t, provider, streamHandler(geminiTextChunk, tc.response))

				drainStream(t, c)

				assertTokenCounts(t, singleRecord(t, c), tc.input, tc.output, tc.reasoning, tc.total)
			})
		}
	}
}

func codeExecutionConfig() *genai.GenerateContentConfig {
	return &genai.GenerateContentConfig{Tools: []*genai.Tool{{CodeExecution: &genai.ToolCodeExecution{}}}}
}
