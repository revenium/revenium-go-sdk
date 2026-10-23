package litellm

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/revenium/revenium-go-sdk/core/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const capturedAnswer = `{"id":"chatcmpl-cap","object":"chat.completion","created":1700000000,"model":"openai/gpt-4.1-nano",` +
	`"choices":[{"index":0,"message":{"role":"assistant","content":"Paris."},"finish_reason":"stop"}],` +
	`"usage":{"prompt_tokens":12,"completion_tokens":2,"total_tokens":14}}`

func streamedAnswer(content string) string {
	chunk, _ := json.Marshal(StreamChunk{ID: "resp-cap", Model: "openai/gpt-4.1-nano", Choices: []StreamChoice{{Delta: StreamDelta{Content: content}}}})
	return "data: " + string(chunk) + "\n\n" +
		"data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\" there\"},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":12,\"completion_tokens\":2,\"total_tokens\":14}}\n\n" +
		"data: [DONE]\n\n"
}

func captureProxy(t *testing.T, streamed string) *httptest.Server {
	t.Helper()
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Stream bool `json:"stream"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.Stream {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte(streamed))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(capturedAnswer))
	}))
	t.Cleanup(proxy.Close)
	return proxy
}

func captureRequest() ChatCompletionRequest {
	return ChatCompletionRequest{
		Model: "openai/gpt-4.1-nano",
		Messages: []ChatMessage{
			{Role: "system", Content: "Be brief."},
			{Role: "user", Content: "Capital of France? sk-ant-abcdefghij1234567890extra"},
		},
	}
}

func liteLLMRecord(t *testing.T, ctx context.Context, streamed string, stream bool) map[string]interface{} {
	t.Helper()
	proxy := captureProxy(t, streamed)
	mock := testutil.NewMockMeteringServer()
	defer mock.Close()
	r := newTestLiteLLM(t, proxy.URL, mock.URL())
	defer r.Close()

	if stream {
		s, err := r.Chat().Completions().NewStreaming(ctx, captureRequest())
		require.NoError(t, err)
		for s.Next() {
		}
		require.NoError(t, s.Close())
	} else {
		_, err := r.Chat().Completions().New(ctx, captureRequest())
		require.NoError(t, err)
	}
	return singlePayload(t, r, mock)
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

	assertCapturedPrompt(t, liteLLMRecord(t, context.Background(), streamedAnswer("hi"), false), "Paris.")
	assertCapturedPrompt(t, liteLLMRecord(t, context.Background(), streamedAnswer("hi"), true), "hi there")
}

func TestPromptCapture_ChatRecordsNoPromptWhenOff(t *testing.T) {
	t.Setenv(core.EnvCapturePrompts, "")

	assertNoCapturedPrompt(t, liteLLMRecord(t, context.Background(), streamedAnswer("hi"), false))
	assertNoCapturedPrompt(t, liteLLMRecord(t, context.Background(), streamedAnswer("hi"), true))
}

func TestPromptCapture_PerCallSettingOverridesTheEnvironment(t *testing.T) {
	t.Setenv(core.EnvCapturePrompts, "true")
	off := core.WithUsageMetadata(context.Background(), map[string]interface{}{"capturePrompts": false})
	assertNoCapturedPrompt(t, liteLLMRecord(t, off, streamedAnswer("hi"), false))
	assertNoCapturedPrompt(t, liteLLMRecord(t, off, streamedAnswer("hi"), true))

	t.Setenv(core.EnvCapturePrompts, "false")
	on := core.WithUsageMetadata(context.Background(), map[string]interface{}{"capturePrompts": true})
	assertCapturedPrompt(t, liteLLMRecord(t, on, streamedAnswer("hi"), false), "Paris.")
	assertCapturedPrompt(t, liteLLMRecord(t, on, streamedAnswer("hi"), true), "hi there")
}

func TestPromptCapture_StreamedReplyOverTheLimitIsCutAndFlagged(t *testing.T) {
	t.Setenv(core.EnvCapturePrompts, "true")
	t.Setenv(core.EnvMaxPromptSize, "300")

	record := liteLLMRecord(t, context.Background(), streamedAnswer(strings.Repeat("é", 2000)), true)

	reply, _ := record["outputResponse"].(string)
	assert.LessOrEqual(t, utf8.RuneCountInString(reply), 300)
	assert.True(t, utf8.ValidString(reply))
	assert.True(t, strings.HasSuffix(reply, "...[TRUNCATED]"))
	assert.Equal(t, true, record["promptsTruncated"])
}

func TestPromptCapture_ReconstructedResponseStaysWithinTheLimit(t *testing.T) {
	t.Setenv(core.EnvCapturePrompts, "true")
	t.Setenv(core.EnvMaxPromptSize, "300")
	chunk, _ := json.Marshal(StreamChunk{Choices: []StreamChoice{{Delta: StreamDelta{Content: strings.Repeat("a", 2000)}}}})
	s := newStreamFromString("data: " + string(chunk) + "\n\ndata: [DONE]\n\n")
	for s.Next() {
	}

	content := s.ReconstructResponse().Choices[0].Message.Content

	assert.Equal(t, 300, utf8.RuneCountInString(content))
	assert.True(t, strings.HasSuffix(content, "...[TRUNCATED]"))
}

func TestPromptCapture_StreamWithSeveralChoicesCapturesTheFirst(t *testing.T) {
	t.Setenv(core.EnvCapturePrompts, "true")
	chunk := func(index int, text string) string {
		encoded, _ := json.Marshal(StreamChunk{Choices: []StreamChoice{{Index: index, Delta: StreamDelta{Content: text}}}})
		return "data: " + string(encoded) + "\n\n"
	}
	stop, _ := json.Marshal(StreamChunk{Choices: []StreamChoice{{Index: 0, FinishReason: "stop"}}})
	streamed := chunk(0, "Par") + chunk(1, "Lyo") + chunk(0, "is") + chunk(1, "n") + "data: " + string(stop) + "\n\ndata: [DONE]\n\n"

	record := liteLLMRecord(t, context.Background(), streamed, true)

	assert.Equal(t, "Paris", record["outputResponse"])
}

func TestPromptCapture_ChoiceZeroIsCapturedWhereverItSitsInTheChunk(t *testing.T) {
	t.Setenv(core.EnvCapturePrompts, "true")
	encoded, _ := json.Marshal(StreamChunk{Choices: []StreamChoice{
		{Index: 1, Delta: StreamDelta{Content: "Lyon"}},
		{Index: 0, Delta: StreamDelta{Content: "Paris"}, FinishReason: "stop"},
	}})

	record := liteLLMRecord(t, context.Background(), "data: "+string(encoded)+"\n\ndata: [DONE]\n\n", true)

	assert.Equal(t, "Paris", record["outputResponse"])
}

func TestPromptCapture_StreamStoppedEarlyIsMarkedTruncated(t *testing.T) {
	t.Setenv(core.EnvCapturePrompts, "true")
	proxy := captureProxy(t, streamedAnswer("hi"))
	mock := testutil.NewMockMeteringServer()
	defer mock.Close()
	r := newTestLiteLLM(t, proxy.URL, mock.URL())
	defer r.Close()

	s, err := r.Chat().Completions().NewStreaming(context.Background(), captureRequest())
	require.NoError(t, err)
	require.True(t, s.Next())
	require.NoError(t, s.Close())

	record := singlePayload(t, r, mock)
	assert.Equal(t, "hi...[TRUNCATED]", record["outputResponse"])
	assert.Equal(t, true, record["promptsTruncated"])
}

func TestPromptCapture_AProxyHeaderCannotTurnCaptureOn(t *testing.T) {
	t.Setenv(core.EnvCapturePrompts, "")
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprintf("streamed=%v", stream), func(t *testing.T) {
			assertNoCapturedPrompt(t, recordWithProxyCaptureHeader(t, "true", stream))
		})
	}
}

// The caller's own usage metadata may carry a systemPrompt for other reasons; with capture
// left off, a proxy header must not get it, or anything else captured, onto the record.
func TestPromptCapture_AProxyHeaderCannotSendTheMetadataSystemPrompt(t *testing.T) {
	t.Setenv(core.EnvCapturePrompts, "")
	ctx := core.WithUsageMetadata(context.Background(), map[string]interface{}{"systemPrompt": "Internal routing note."})
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprintf("streamed=%v", stream), func(t *testing.T) {
			assertNoCapturedPrompt(t, recordWithProxyCaptureHeaderFor(t, ctx, "true", stream))
		})
	}
}

func TestPromptCapture_AProxyHeaderCannotTurnCaptureOff(t *testing.T) {
	t.Setenv(core.EnvCapturePrompts, "true")
	for stream, reply := range map[bool]string{false: "Paris.", true: "hi there"} {
		t.Run(fmt.Sprintf("streamed=%v", stream), func(t *testing.T) {
			assertCapturedPrompt(t, recordWithProxyCaptureHeader(t, "false", stream), reply)
		})
	}
}

func recordWithProxyCaptureHeader(t *testing.T, header string, stream bool) map[string]interface{} {
	t.Helper()
	return recordWithProxyCaptureHeaderFor(t, context.Background(), header, stream)
}

func recordWithProxyCaptureHeaderFor(t *testing.T, ctx context.Context, header string, stream bool) map[string]interface{} {
	t.Helper()
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Revenium-Capture-Prompts", header)
		if stream {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte(streamedAnswer("hi")))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(capturedAnswer))
	}))
	defer proxy.Close()
	mock := testutil.NewMockMeteringServer()
	defer mock.Close()
	r := newTestLiteLLM(t, proxy.URL, mock.URL())
	defer r.Close()

	if stream {
		s, err := r.Chat().Completions().NewStreaming(ctx, captureRequest())
		require.NoError(t, err)
		for s.Next() {
		}
		require.NoError(t, s.Close())
	} else {
		_, err := r.Chat().Completions().New(ctx, captureRequest())
		require.NoError(t, err)
	}
	return singlePayload(t, r, mock)
}

func TestPromptCapture_AnotherChoiceFinishingDoesNotCompleteChoiceZero(t *testing.T) {
	t.Setenv(core.EnvCapturePrompts, "true")
	chunk := func(choices ...StreamChoice) string {
		encoded, _ := json.Marshal(StreamChunk{Choices: choices})
		return "data: " + string(encoded) + "\n\n"
	}
	streamed := chunk(StreamChoice{Index: 1, Delta: StreamDelta{Content: "Lyon"}, FinishReason: "stop"},
		StreamChoice{Index: 0, Delta: StreamDelta{Content: "Par"}}) +
		chunk(StreamChoice{Index: 0, Delta: StreamDelta{Content: "is"}, FinishReason: "stop"}) +
		"data: [DONE]\n\n"
	proxy := captureProxy(t, streamed)
	mock := testutil.NewMockMeteringServer()
	defer mock.Close()
	r := newTestLiteLLM(t, proxy.URL, mock.URL())
	defer r.Close()

	s, err := r.Chat().Completions().NewStreaming(context.Background(), captureRequest())
	require.NoError(t, err)
	require.True(t, s.Next())
	require.NoError(t, s.Close())

	record := singlePayload(t, r, mock)
	assert.Equal(t, "Par...[TRUNCATED]", record["outputResponse"])
	assert.Equal(t, true, record["promptsTruncated"])
}

func TestPromptCapture_ChoiceZeroFinishingCompletesTheReplyWhenTheCallerStopsThere(t *testing.T) {
	t.Setenv(core.EnvCapturePrompts, "true")
	proxy := captureProxy(t, streamedAnswer("hi"))
	mock := testutil.NewMockMeteringServer()
	defer mock.Close()
	r := newTestLiteLLM(t, proxy.URL, mock.URL())
	defer r.Close()

	s, err := r.Chat().Completions().NewStreaming(context.Background(), captureRequest())
	require.NoError(t, err)
	require.True(t, s.Next())
	require.True(t, s.Next())
	require.NoError(t, s.Close())

	record := singlePayload(t, r, mock)
	assert.Equal(t, "hi there", record["outputResponse"])
	assert.NotContains(t, record, "promptsTruncated")
}

func cutBasicCredential() string {
	credential := base64.StdEncoding.EncodeToString([]byte("admin:" + strings.Repeat("p", 60)))
	return credential[:len(credential)-3]
}

func choiceZeroChunk(text, finishReason string) string {
	encoded, _ := json.Marshal(StreamChunk{Choices: []StreamChoice{{Delta: StreamDelta{Content: text}, FinishReason: finishReason}}})
	return "data: " + string(encoded) + "\n\n"
}

func TestPromptCapture_StreamThatEndsCleanlyBeforeItsFinishReasonIsTruncatedAndMasksACutCredential(t *testing.T) {
	t.Setenv(core.EnvCapturePrompts, "true")
	cut := cutBasicCredential()
	endings := map[string]string{"transport close": "", "done marker": "data: [DONE]\n\n"}
	for name, ending := range endings {
		t.Run(name, func(t *testing.T) {
			record := liteLLMRecord(t, context.Background(), choiceZeroChunk("Use Basic "+cut, "")+ending, true)

			assert.Equal(t, true, record["promptsTruncated"])
			reply := record["outputResponse"].(string)
			assert.Contains(t, reply, `Basic ***REDACTED***`)
			assert.NotContains(t, reply, cut[:24])
		})
	}
}

func TestPromptCapture_StreamReadToItsFinishReasonIsComplete(t *testing.T) {
	t.Setenv(core.EnvCapturePrompts, "true")

	record := liteLLMRecord(t, context.Background(), choiceZeroChunk("Checking.", "stop")+"data: [DONE]\n\n", true)

	assert.Equal(t, "Checking.", record["outputResponse"])
	assert.NotContains(t, record, "promptsTruncated")
}
