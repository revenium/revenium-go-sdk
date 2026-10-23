package google

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/genai"

	"github.com/revenium/revenium-go-sdk/core"
)

const geminiAnswer = `{"candidates":[{"content":{"parts":[{"text":"thinking...","thought":true},{"text":"Paris."},{"text":"Lovely city."}],"role":"model"},"finishReason":"STOP","index":0}],` +
	`"usageMetadata":{"promptTokenCount":9,"candidatesTokenCount":4,"totalTokenCount":13},"modelVersion":"gemini-3.5-flash-lite"}`

func answerHandler(w http.ResponseWriter, r *http.Request) {
	if isStreamRequest(r) {
		writeSSE(w, geminiTextChunk, geminiMaxTokensChunk)
		return
	}
	writeJSON(w, http.StatusOK, geminiAnswer)
}

func captureContents() []*genai.Content {
	return []*genai.Content{
		genai.NewContentFromText("Capital of France? key sk-ant-abcdefghij1234567890extra", genai.RoleUser),
		{Role: genai.RoleUser, Parts: []*genai.Part{genai.NewPartFromBytes([]byte{0x89, 0x50}, "image/png")}},
	}
}

func captureConfig() *genai.GenerateContentConfig {
	return &genai.GenerateContentConfig{SystemInstruction: genai.NewContentFromText("Answer briefly.", genai.RoleUser)}
}

func generate(t *testing.T, ctx context.Context, c *stubbedClient) {
	t.Helper()
	_, err := c.client.Models().GenerateContent(ctx, "gemini-3.5-flash-lite", captureContents(), captureConfig())
	require.NoError(t, err)
}

func generateStream(t *testing.T, ctx context.Context, c *stubbedClient) {
	t.Helper()
	for _, err := range c.client.Models().GenerateContentStream(ctx, "gemini-3.5-flash-lite", captureContents(), captureConfig()) {
		require.NoError(t, err)
	}
}

func capturedInput(t *testing.T, record map[string]interface{}) []map[string]interface{} {
	t.Helper()
	text, ok := record["inputMessages"].(string)
	require.True(t, ok, "inputMessages missing from %v", record)
	var messages []map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(text), &messages))
	return messages
}

func assertNoCapturedPrompt(t *testing.T, record map[string]interface{}) {
	t.Helper()
	for _, field := range []string{"systemPrompt", "inputMessages", "outputResponse", "promptsTruncated"} {
		assert.NotContains(t, record, field)
	}
}

func TestPromptCapture_GenerateContentRecordsPromptAndReplyWhenOn(t *testing.T) {
	t.Setenv(core.EnvCapturePrompts, "true")
	for _, provider := range streamProviders {
		t.Run(provider.String(), func(t *testing.T) {
			c := newStubbedClient(t, provider, answerHandler)

			generate(t, context.Background(), c)

			record := singleRecord(t, c)
			assert.Equal(t, "Answer briefly.", record["systemPrompt"])
			messages := capturedInput(t, record)
			require.Len(t, messages, 2)
			text := messages[0]["parts"].([]interface{})[0].(map[string]interface{})["text"]
			assert.Contains(t, text, "Capital of France?")
			assert.NotContains(t, text, "abcdefghij1234567890extra")
			assert.Equal(t, []interface{}{map[string]interface{}{"inline_data": "[BINARY_DATA]"}}, messages[1]["parts"])
			assert.Equal(t, "Paris.\nLovely city.", record["outputResponse"])
		})
	}
}

func TestPromptCapture_GenerateContentStreamRecordsTheCollectedReplyWhenOn(t *testing.T) {
	t.Setenv(core.EnvCapturePrompts, "true")
	for _, provider := range streamProviders {
		t.Run(provider.String(), func(t *testing.T) {
			c := newStubbedClient(t, provider, answerHandler)

			generateStream(t, context.Background(), c)

			record := singleRecord(t, c)
			assert.Equal(t, "Answer briefly.", record["systemPrompt"])
			assert.Len(t, capturedInput(t, record), 2)
			assert.Equal(t, "Once upon a time a fox", record["outputResponse"])
		})
	}
}

func TestPromptCapture_GenerateContentRecordsNoPromptWhenOff(t *testing.T) {
	t.Setenv(core.EnvCapturePrompts, "")
	c := newStubbedClient(t, ProviderGoogleAI, answerHandler)
	generate(t, context.Background(), c)
	assertNoCapturedPrompt(t, singleRecord(t, c))

	streamed := newStubbedClient(t, ProviderGoogleAI, answerHandler)
	generateStream(t, context.Background(), streamed)
	assertNoCapturedPrompt(t, singleRecord(t, streamed))
}

func TestPromptCapture_PerCallSettingOverridesTheEnvironment(t *testing.T) {
	t.Setenv(core.EnvCapturePrompts, "true")
	off := core.WithUsageMetadata(context.Background(), map[string]interface{}{"capturePrompts": false})
	c := newStubbedClient(t, ProviderGoogleAI, answerHandler)
	generate(t, off, c)
	assertNoCapturedPrompt(t, singleRecord(t, c))

	t.Setenv(core.EnvCapturePrompts, "false")
	on := core.WithUsageMetadata(context.Background(), map[string]interface{}{"capturePrompts": true})
	streamed := newStubbedClient(t, ProviderGoogleAI, answerHandler)
	generateStream(t, on, streamed)
	assert.Equal(t, "Once upon a time a fox", singleRecord(t, streamed)["outputResponse"])
}

func TestPromptCapture_ChatTurnsRecordTheHistorySent(t *testing.T) {
	t.Setenv(core.EnvCapturePrompts, "true")
	c := newStubbedClient(t, ProviderGoogleAI, answerHandler)
	chat, err := c.client.Chats().Create(context.Background(), "gemini-3.5-flash-lite", nil, nil)
	require.NoError(t, err)

	_, err = chat.SendMessage(context.Background(), genai.Part{Text: "first"})
	require.NoError(t, err)
	_, err = chat.SendMessage(context.Background(), genai.Part{Text: "second"})
	require.NoError(t, err)

	records := c.records(t)
	require.Len(t, records, 2)
	sizes := []int{len(capturedInput(t, records[0])), len(capturedInput(t, records[1]))}
	assert.ElementsMatch(t, []int{1, 3}, sizes)
	for _, record := range records {
		assert.NotContains(t, record["inputMessages"], "thinking...")
	}
}

func TestPromptCapture_StreamWithSeveralCandidatesCapturesTheFirst(t *testing.T) {
	t.Setenv(core.EnvCapturePrompts, "true")
	candidate := func(index int, text string) string {
		return fmt.Sprintf(`{"candidates":[{"content":{"parts":[{"text":%q}],"role":"model"},"index":%d}],"modelVersion":"gemini-3.5-flash-lite"}`, text, index)
	}
	c := newStubbedClient(t, ProviderGoogleAI, streamHandler(candidate(1, "Lyo"), candidate(0, "Par"), candidate(1, "n"), candidate(0, "is"), geminiStopChunk, geminiUsageOnlyChunk))

	generateStream(t, context.Background(), c)

	assert.Equal(t, "Paris", singleRecord(t, c)["outputResponse"])
}

func TestPromptCapture_StreamStoppedEarlyIsMarkedTruncated(t *testing.T) {
	t.Setenv(core.EnvCapturePrompts, "true")
	c := newStubbedClient(t, ProviderGoogleAI, answerHandler)

	for _, err := range c.client.Models().GenerateContentStream(context.Background(), "gemini-3.5-flash-lite", captureContents(), captureConfig()) {
		require.NoError(t, err)
		break
	}

	record := singleRecord(t, c)
	assert.Equal(t, "Once upon a time...[TRUNCATED]", record["outputResponse"])
	assert.Equal(t, true, record["promptsTruncated"])
}

func cutBasicCredential() string {
	credential := base64.StdEncoding.EncodeToString([]byte("admin:" + strings.Repeat("p", 60)))
	return credential[:len(credential)-3]
}

func textChunk(text, finishReason string) string {
	return fmt.Sprintf(`{"candidates":[{"content":{"parts":[{"text":%q}],"role":"model"},%s"index":0}],"modelVersion":"gemini-3.5-flash-lite"}`, text, finishReason)
}

func TestPromptCapture_StreamThatEndsCleanlyBeforeItsFinishReasonIsTruncatedAndMasksACutCredential(t *testing.T) {
	t.Setenv(core.EnvCapturePrompts, "true")
	cut := cutBasicCredential()
	for _, provider := range streamProviders {
		t.Run(provider.String(), func(t *testing.T) {
			c := newStubbedClient(t, provider, streamHandler(textChunk("Use Basic "+cut, ""), geminiUsageOnlyChunk))

			generateStream(t, context.Background(), c)

			record := singleRecord(t, c)
			assert.Equal(t, true, record["promptsTruncated"])
			reply := record["outputResponse"].(string)
			assert.Contains(t, reply, `Basic ***REDACTED***`)
			assert.NotContains(t, reply, cut[:24])
		})
	}
}

func TestPromptCapture_StreamReadToItsFinishReasonIsComplete(t *testing.T) {
	t.Setenv(core.EnvCapturePrompts, "true")
	for _, provider := range streamProviders {
		t.Run(provider.String(), func(t *testing.T) {
			c := newStubbedClient(t, provider, streamHandler(textChunk("Checking.", `"finishReason":"STOP",`), geminiUsageOnlyChunk))

			generateStream(t, context.Background(), c)

			record := singleRecord(t, c)
			assert.Equal(t, "Checking.", record["outputResponse"])
			assert.NotContains(t, record, "promptsTruncated")
		})
	}
}
