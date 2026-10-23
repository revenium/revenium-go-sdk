package ollama

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/revenium/revenium-go-sdk/core/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const capturedAnswer = `{"id":"chatcmpl-cap","object":"chat.completion","created":1700000000,"model":"llama3.2",` +
	`"choices":[{"index":0,"message":{"role":"assistant","content":"Paris."},"finish_reason":"stop"}],` +
	`"usage":{"prompt_tokens":12,"completion_tokens":2,"total_tokens":14}}`

func capturedRecord(t *testing.T, ctx context.Context) map[string]interface{} {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(capturedAnswer))
	}))
	defer server.Close()
	mock := testutil.NewMockMeteringServer()
	defer mock.Close()
	r := newSubscriberTestOllama(t, server.URL, mock.URL())
	defer r.Close()

	_, err := r.ChatCompletions(ctx, ChatCompletionRequest{
		Model: "llama3.2",
		Messages: []ChatMessage{
			{Role: "system", Content: "Be brief."},
			{Role: "user", Content: "Capital of France? sk-ant-abcdefghij1234567890extra"},
		},
	})
	require.NoError(t, err)
	r.Flush()

	require.True(t, mock.WaitForPayloads(1, 2*time.Second))
	payloads := mock.GetPayloads()
	require.Len(t, payloads, 1)
	return payloads[0]
}

func assertNoCapturedPrompt(t *testing.T, record map[string]interface{}) {
	t.Helper()
	for _, field := range []string{"systemPrompt", "inputMessages", "outputResponse", "promptsTruncated"} {
		assert.NotContains(t, record, field)
	}
}

func assertCapturedPrompt(t *testing.T, record map[string]interface{}) {
	t.Helper()
	assert.Equal(t, "Be brief.", record["systemPrompt"])
	text, _ := record["inputMessages"].(string)
	var messages []map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(text), &messages), "inputMessages missing from %v", record)
	require.Len(t, messages, 1)
	assert.Equal(t, "user", messages[0]["role"])
	assert.Contains(t, messages[0]["content"], "Capital of France?")
	assert.NotContains(t, messages[0]["content"], "abcdefghij1234567890extra")
	assert.Equal(t, "Paris.", record["outputResponse"])
}

func TestPromptCapture_ChatRecordsPromptAndReplyWhenOn(t *testing.T) {
	t.Setenv(core.EnvCapturePrompts, "true")

	assertCapturedPrompt(t, capturedRecord(t, context.Background()))
}

func TestPromptCapture_ChatRecordsNoPromptWhenOff(t *testing.T) {
	t.Setenv(core.EnvCapturePrompts, "")

	assertNoCapturedPrompt(t, capturedRecord(t, context.Background()))
}

func TestPromptCapture_PerCallSettingOverridesTheEnvironment(t *testing.T) {
	t.Setenv(core.EnvCapturePrompts, "true")
	assertNoCapturedPrompt(t, capturedRecord(t, core.WithUsageMetadata(context.Background(), map[string]interface{}{"capturePrompts": false})))

	t.Setenv(core.EnvCapturePrompts, "false")
	assertCapturedPrompt(t, capturedRecord(t, core.WithUsageMetadata(context.Background(), map[string]interface{}{"capturePrompts": true})))
}
