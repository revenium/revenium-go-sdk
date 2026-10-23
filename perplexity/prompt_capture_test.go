package perplexity

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	openai "github.com/openai/openai-go/v3"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/revenium/revenium-go-sdk/core/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const capturedAnswer = `{"id":"pplx-cap","object":"chat.completion","created":1700000000,"model":"sonar","choices":[{"index":0,"message":{"role":"assistant","content":"Paris."},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":2,"total_tokens":12}}`

func captureParams() openai.ChatCompletionNewParams {
	return openai.ChatCompletionNewParams{
		Model: "sonar",
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage("Be brief."),
			openai.UserMessage("Capital of France? pplx-abcdefghij1234567890extra"),
		},
	}
}

func recordOf(t *testing.T, provider http.Handler, call func(r *ReveniumPerplexity)) map[string]interface{} {
	t.Helper()
	server := httptest.NewServer(provider)
	defer server.Close()
	mock := testutil.NewMockMeteringServer()
	defer mock.Close()
	r := newTestPerplexity(t, server.URL, mock.URL())
	defer r.Close()

	call(r)
	r.Flush()

	require.True(t, mock.WaitForPayloads(1, 2*time.Second))
	payloads := mock.GetPayloads()
	require.Len(t, payloads, 1)
	return payloads[0]
}

func plainCall(t *testing.T, ctx context.Context) func(r *ReveniumPerplexity) {
	return func(r *ReveniumPerplexity) {
		_, err := r.Chat().Completions().New(ctx, captureParams())
		require.NoError(t, err)
	}
}

func streamedCall(t *testing.T, ctx context.Context) func(r *ReveniumPerplexity) {
	return func(r *ReveniumPerplexity) {
		stream, err := r.Chat().Completions().NewStreaming(ctx, captureParams())
		require.NoError(t, err)
		for stream.Next() {
			stream.Current()
		}
		require.NoError(t, stream.Close())
	}
}

func answer(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(capturedAnswer))
}

func assertNoCapturedPrompt(t *testing.T, record map[string]interface{}) {
	t.Helper()
	for _, field := range []string{"systemPrompt", "inputMessages", "outputResponse", "promptsTruncated"} {
		assert.NotContains(t, record, field)
	}
}

func assertCapturedPrompt(t *testing.T, record map[string]interface{}, reply string) {
	t.Helper()
	assert.Equal(t, "Be brief.", record["systemPrompt"])
	text, _ := record["inputMessages"].(string)
	var messages []map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(text), &messages), "inputMessages missing from %v", record)
	require.Len(t, messages, 1)
	assert.Contains(t, messages[0]["content"], "Capital of France?")
	assert.NotContains(t, messages[0]["content"], "abcdefghij1234567890extra")
	assert.Equal(t, reply, record["outputResponse"])
}

func TestPromptCapture_ChatRecordsPromptAndReplyWhenOn(t *testing.T) {
	t.Setenv(core.EnvCapturePrompts, "true")

	assertCapturedPrompt(t, recordOf(t, http.HandlerFunc(answer), plainCall(t, context.Background())), "Paris.")
	assertCapturedPrompt(t, recordOf(t, sseChatHandler(t), streamedCall(t, context.Background())), "hi there")
}

func TestPromptCapture_ChatRecordsNoPromptWhenOff(t *testing.T) {
	t.Setenv(core.EnvCapturePrompts, "")

	assertNoCapturedPrompt(t, recordOf(t, http.HandlerFunc(answer), plainCall(t, context.Background())))
	assertNoCapturedPrompt(t, recordOf(t, sseChatHandler(t), streamedCall(t, context.Background())))
}

func TestPromptCapture_PerCallSettingOverridesTheEnvironment(t *testing.T) {
	t.Setenv(core.EnvCapturePrompts, "true")
	off := core.WithUsageMetadata(context.Background(), map[string]interface{}{"capturePrompts": false})
	assertNoCapturedPrompt(t, recordOf(t, http.HandlerFunc(answer), plainCall(t, off)))
	assertNoCapturedPrompt(t, recordOf(t, sseChatHandler(t), streamedCall(t, off)))

	t.Setenv(core.EnvCapturePrompts, "false")
	on := core.WithUsageMetadata(context.Background(), map[string]interface{}{"capturePrompts": true})
	assertCapturedPrompt(t, recordOf(t, http.HandlerFunc(answer), plainCall(t, on)), "Paris.")
	assertCapturedPrompt(t, recordOf(t, sseChatHandler(t), streamedCall(t, on)), "hi there")
}

func TestStreamRecordIsTheSameWhateverTheCallerReads(t *testing.T) {
	t.Setenv(core.EnvCapturePrompts, "true")
	reads := map[string]func(stream *StreamingWrapper){
		"next only":     func(stream *StreamingWrapper) {},
		"current once":  func(stream *StreamingWrapper) { stream.Current() },
		"current twice": func(stream *StreamingWrapper) { stream.Current(); stream.Current() },
	}
	for name, read := range reads {
		t.Run(name, func(t *testing.T) {
			record := recordOf(t, sseChatHandler(t), func(r *ReveniumPerplexity) {
				stream, err := r.Chat().Completions().NewStreaming(context.Background(), captureParams())
				require.NoError(t, err)
				for stream.Next() {
					read(stream)
				}
				require.NoError(t, stream.Close())
			})

			assert.Equal(t, float64(7), record["inputTokenCount"])
			assert.Equal(t, float64(2), record["outputTokenCount"])
			assert.Equal(t, float64(9), record["totalTokenCount"])
			assert.Equal(t, "END", record["stopReason"])
			assert.Equal(t, 0.0003, record["totalCost"])
			assert.Equal(t, "hi there", record["outputResponse"])
		})
	}
}

func TestPromptCapture_StreamStoppedEarlyIsMarkedTruncated(t *testing.T) {
	t.Setenv(core.EnvCapturePrompts, "true")

	record := recordOf(t, sseChatHandler(t), func(r *ReveniumPerplexity) {
		stream, err := r.Chat().Completions().NewStreaming(context.Background(), captureParams())
		require.NoError(t, err)
		require.True(t, stream.Next())
		require.NoError(t, stream.Close())
	})

	assert.Equal(t, "hi...[TRUNCATED]", record["outputResponse"])
	assert.Equal(t, true, record["promptsTruncated"])
}

func cutBasicCredential() string {
	credential := base64.StdEncoding.EncodeToString([]byte("admin:" + strings.Repeat("p", 60)))
	return credential[:len(credential)-3]
}

func sonarChunk(text, finishReason string) string {
	encoded, _ := json.Marshal(text)
	return fmt.Sprintf(`data: {"id":"pplx-1","object":"chat.completion.chunk","created":1700000000,"model":"sonar","choices":[{"index":0,"delta":{"content":%s},"finish_reason":%s}]}`+"\n\n", encoded, finishReason)
}

func sseEvents(t *testing.T, events ...string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for _, event := range events {
			writeSSE(t, w, event)
		}
	})
}

func TestPromptCapture_StreamThatEndsCleanlyBeforeItsFinishReasonIsTruncatedAndMasksACutCredential(t *testing.T) {
	t.Setenv(core.EnvCapturePrompts, "true")
	cut := cutBasicCredential()

	record := recordOf(t, sseEvents(t, sonarChunk("Use Basic "+cut, "null")), streamedCall(t, context.Background()))

	assert.Equal(t, true, record["promptsTruncated"])
	reply := record["outputResponse"].(string)
	assert.Contains(t, reply, `Basic ***REDACTED***`)
	assert.NotContains(t, reply, cut[:24])
}

func TestPromptCapture_StreamReadToItsFinishReasonIsComplete(t *testing.T) {
	t.Setenv(core.EnvCapturePrompts, "true")

	record := recordOf(t, sseEvents(t, sonarChunk("Checking.", `"stop"`), "data: [DONE]\n\n"), streamedCall(t, context.Background()))

	assert.Equal(t, "Checking.", record["outputResponse"])
	assert.NotContains(t, record, "promptsTruncated")
}
