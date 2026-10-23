package openai

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

	openaisdk "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/revenium/revenium-go-sdk/core/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const capturedChatAnswer = `{"id":"chatcmpl-cap","object":"chat.completion","created":1700000000,"model":"gpt-4.1-nano","choices":[{"index":0,"message":{"role":"assistant","content":"Paris."},"finish_reason":"stop"}],"usage":{"prompt_tokens":20,"completion_tokens":2,"total_tokens":22}}`

const capturedResponsesAnswer = `{"id":"resp_cap","object":"response","created_at":1700000000,"model":"gpt-4.1-nano","status":"completed","output":[{"type":"message","id":"msg_1","role":"assistant","status":"completed","content":[{"type":"output_text","text":"Paris.","annotations":[]}]}],"usage":{"input_tokens":9,"input_tokens_details":{"cached_tokens":0},"output_tokens":2,"output_tokens_details":{"reasoning_tokens":0},"total_tokens":11}}`

const capturedResponsesStreamCompleted = `{"type":"response.completed","sequence_number":2,"response":` + capturedResponsesAnswer + `}`

func captureChatParams() openaisdk.ChatCompletionNewParams {
	return openaisdk.ChatCompletionNewParams{
		Model: "gpt-4.1-nano",
		Messages: []openaisdk.ChatCompletionMessageParamUnion{
			openaisdk.SystemMessage("Answer in one word."),
			openaisdk.UserMessage("Capital of France? my key is sk-ant-abcdefghij1234567890extra"),
		},
	}
}

func jsonAnswer(body string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	})
}

func recordOf(t *testing.T, provider http.Handler, call func(r *ReveniumOpenAI)) map[string]interface{} {
	t.Helper()
	server := httptest.NewServer(provider)
	defer server.Close()
	mock := testutil.NewMockMeteringServer()
	defer mock.Close()

	r := newTestOpenAI(t, server.URL, mock.URL())
	defer r.Close()
	call(r)
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

func capturedInput(t *testing.T, record map[string]interface{}) []map[string]interface{} {
	t.Helper()
	text, ok := record["inputMessages"].(string)
	require.True(t, ok, "inputMessages missing from %v", record)
	var messages []map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(text), &messages))
	return messages
}

func chatCall(t *testing.T, ctx context.Context) func(r *ReveniumOpenAI) {
	return func(r *ReveniumOpenAI) {
		_, err := r.Chat().Completions().New(ctx, captureChatParams())
		require.NoError(t, err)
	}
}

func streamedChatCall(t *testing.T, ctx context.Context) func(r *ReveniumOpenAI) {
	return func(r *ReveniumOpenAI) {
		stream, err := r.Chat().Completions().NewStreaming(ctx, captureChatParams())
		require.NoError(t, err)
		for stream.Next() {
		}
		require.NoError(t, stream.Close())
	}
}

func TestPromptCapture_ChatRecordsPromptAndReplyWhenOn(t *testing.T) {
	t.Setenv(core.EnvCapturePrompts, "true")

	record := recordOf(t, jsonAnswer(capturedChatAnswer), chatCall(t, context.Background()))

	assert.Equal(t, "Answer in one word.", record["systemPrompt"])
	messages := capturedInput(t, record)
	require.Len(t, messages, 1)
	assert.Equal(t, "user", messages[0]["role"])
	assert.Contains(t, messages[0]["content"], "Capital of France?")
	assert.NotContains(t, messages[0]["content"], "abcdefghij1234567890extra")
	assert.Equal(t, "Paris.", record["outputResponse"])
}

func TestPromptCapture_StreamedChatRecordsTheCollectedReplyWhenOn(t *testing.T) {
	t.Setenv(core.EnvCapturePrompts, "true")

	record := recordOf(t, sseChatHandler(t), streamedChatCall(t, context.Background()))

	assert.Equal(t, "Answer in one word.", record["systemPrompt"])
	assert.Len(t, capturedInput(t, record), 1)
	assert.Equal(t, "hello world", record["outputResponse"])
}

func TestPromptCapture_ChatRecordsNoPromptWhenOff(t *testing.T) {
	t.Setenv(core.EnvCapturePrompts, "")

	assertNoCapturedPrompt(t, recordOf(t, jsonAnswer(capturedChatAnswer), chatCall(t, context.Background())))
	assertNoCapturedPrompt(t, recordOf(t, sseChatHandler(t), streamedChatCall(t, context.Background())))
}

func TestPromptCapture_PerCallSettingOverridesTheEnvironment(t *testing.T) {
	t.Setenv(core.EnvCapturePrompts, "true")
	off := core.WithUsageMetadata(context.Background(), map[string]interface{}{"capturePrompts": false})
	assertNoCapturedPrompt(t, recordOf(t, jsonAnswer(capturedChatAnswer), chatCall(t, off)))
	assertNoCapturedPrompt(t, recordOf(t, sseChatHandler(t), streamedChatCall(t, off)))

	t.Setenv(core.EnvCapturePrompts, "false")
	on := core.WithUsageMetadata(context.Background(), map[string]interface{}{"capturePrompts": true})
	assert.Equal(t, "Paris.", recordOf(t, jsonAnswer(capturedChatAnswer), chatCall(t, on))["outputResponse"])
	assert.Equal(t, "hello world", recordOf(t, sseChatHandler(t), streamedChatCall(t, on))["outputResponse"])
}

func TestPromptCapture_AzureChatRecordsPromptAndReply(t *testing.T) {
	t.Setenv(core.EnvCapturePrompts, "true")
	server := httptest.NewTLSServer(jsonAnswer(capturedChatAnswer))
	defer server.Close()
	mock := testutil.NewMockMeteringServer()
	defer mock.Close()
	r := newTestAzureOpenAI(t, server, mock.URL())
	defer r.Close()

	_, err := r.Chat().Completions().New(context.Background(), captureChatParams())
	require.NoError(t, err)
	r.Flush()

	require.True(t, mock.WaitForPayloads(1, 2*time.Second))
	record := mock.GetPayloads()[0]
	assert.Equal(t, "Paris.", record["outputResponse"])
	assert.Equal(t, "Answer in one word.", record["systemPrompt"])
}

func responsesCaptureParams() responses.ResponseNewParams {
	return responses.ResponseNewParams{
		Model:        "gpt-4.1-nano",
		Instructions: openaisdk.String("Answer in one word."),
		Input:        responses.ResponseNewParamsInputUnion{OfString: openaisdk.String("Capital of France?")},
	}
}

func TestPromptCapture_ResponsesRecordsInstructionsInputAndReply(t *testing.T) {
	t.Setenv(core.EnvCapturePrompts, "true")

	record := recordOf(t, jsonAnswer(capturedResponsesAnswer), func(r *ReveniumOpenAI) {
		_, err := r.Responses().Create(context.Background(), responsesCaptureParams())
		require.NoError(t, err)
	})

	assert.Equal(t, "Answer in one word.", record["systemPrompt"])
	assert.Equal(t, []map[string]interface{}{{"role": "user", "content": "Capital of France?"}}, capturedInput(t, record))
	assert.Equal(t, "Paris.", record["outputResponse"])
}

func TestPromptCapture_StreamedResponsesRecordTheFinalReply(t *testing.T) {
	t.Setenv(core.EnvCapturePrompts, "true")

	record := recordOf(t, sseResponsesHandler(t, responsesTextDelta, capturedResponsesStreamCompleted), func(r *ReveniumOpenAI) {
		stream, err := r.Responses().CreateStreaming(context.Background(), responsesCaptureParams())
		require.NoError(t, err)
		for stream.Next() {
		}
		require.NoError(t, stream.Close())
	})

	assert.Equal(t, "Answer in one word.", record["systemPrompt"])
	assert.Equal(t, "Paris.", record["outputResponse"])
}

func TestPromptCapture_ResponsesTakeADeveloperItemAsTheSystemPrompt(t *testing.T) {
	params := responses.ResponseNewParams{Input: responses.ResponseNewParamsInputUnion{OfInputItemList: responses.ResponseInputParam{
		responses.ResponseInputItemParamOfMessage("Be brief.", responses.EasyInputMessageRoleDeveloper),
		responses.ResponseInputItemParamOfMessage("Hi", responses.EasyInputMessageRoleUser),
	}}}

	captured := responsesCapture(params, "Hello")

	assert.Equal(t, "Be brief.", captured.SystemPrompt)
	require.Len(t, captured.Messages, 1)
	assert.Equal(t, "Hi", captured.Messages[0].(map[string]interface{})["content"])
}

func TestPromptCapture_ResponsesRecordNoPromptWhenOff(t *testing.T) {
	t.Setenv(core.EnvCapturePrompts, "")

	assertNoCapturedPrompt(t, recordOf(t, jsonAnswer(capturedResponsesAnswer), func(r *ReveniumOpenAI) {
		_, err := r.Responses().Create(context.Background(), responsesCaptureParams())
		require.NoError(t, err)
	}))
	assertNoCapturedPrompt(t, recordOf(t, sseResponsesHandler(t, responsesTextDelta, capturedResponsesStreamCompleted), func(r *ReveniumOpenAI) {
		stream, err := r.Responses().CreateStreaming(context.Background(), responsesCaptureParams())
		require.NoError(t, err)
		for stream.Next() {
		}
		require.NoError(t, stream.Close())
	}))
}

func TestPromptCapture_TextCompletionsRecordPromptAndReply(t *testing.T) {
	t.Setenv(core.EnvCapturePrompts, "true")
	stub := newTextCompletionsStub(t)

	plain := recordOf(t, stub.server.Config.Handler, func(r *ReveniumOpenAI) {
		_, err := r.Completions().New(context.Background(), textCompletionParams())
		require.NoError(t, err)
	})
	streamed := recordOf(t, stub.server.Config.Handler, func(r *ReveniumOpenAI) {
		stream, err := r.Completions().NewStreaming(context.Background(), textCompletionParams())
		require.NoError(t, err)
		for stream.Next() {
		}
		require.NoError(t, stream.Close())
	})

	for _, record := range []map[string]interface{}{plain, streamed} {
		assert.Equal(t, []map[string]interface{}{{"role": "user", "content": "say hi"}}, capturedInput(t, record))
		assert.NotEmpty(t, record["outputResponse"])
	}
	assert.Equal(t, "hi", streamed["outputResponse"])
}

func TestPromptCapture_TextCompletionsRecordNoPromptWhenOff(t *testing.T) {
	t.Setenv(core.EnvCapturePrompts, "")
	stub := newTextCompletionsStub(t)

	assertNoCapturedPrompt(t, recordOf(t, stub.server.Config.Handler, func(r *ReveniumOpenAI) {
		_, err := r.Completions().New(context.Background(), textCompletionParams())
		require.NoError(t, err)
	}))
}

const failedResponsesAnswer = `{"id":"resp_failed","object":"response","created_at":1700000000,"model":"gpt-4.1-nano","status":"failed",` +
	`"error":{"code":"server_error","message":"The model failed to finish the response."},"output":[],` +
	`"usage":{"input_tokens":12,"input_tokens_details":{"cached_tokens":0},"output_tokens":6,"output_tokens_details":{"reasoning_tokens":0},"total_tokens":18}}`

func TestPromptCapture_FailedResponsesCarryNoCapturedPrompt(t *testing.T) {
	t.Setenv(core.EnvCapturePrompts, "true")

	plain := recordOf(t, jsonAnswer(failedResponsesAnswer), func(r *ReveniumOpenAI) {
		_, err := r.Responses().Create(context.Background(), responsesCaptureParams())
		require.NoError(t, err)
	})
	streamed := recordOf(t, sseResponsesHandler(t, responsesTextDelta, responsesFailed), func(r *ReveniumOpenAI) {
		stream, err := r.Responses().CreateStreaming(context.Background(), responsesCaptureParams())
		require.NoError(t, err)
		for stream.Next() {
		}
		require.NoError(t, stream.Close())
	})

	for _, record := range []map[string]interface{}{plain, streamed} {
		assert.Equal(t, "ERROR", record["stopReason"])
		assertNoCapturedPrompt(t, record)
	}
}

func TestPromptCapture_StreamWithSeveralChoicesCapturesTheFirst(t *testing.T) {
	t.Setenv(core.EnvCapturePrompts, "true")
	twoChoices := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for _, part := range []struct{ index, text string }{{"0", "Par"}, {"1", "Lyo"}, {"0", "is"}, {"1", "n"}} {
			writeSSE(t, w, `data: {"id":"c","object":"chat.completion.chunk","created":1,"model":"gpt-4.1-nano","choices":[{"index":`+part.index+`,"delta":{"content":"`+part.text+`"},"finish_reason":null}]}`+"\n\n")
		}
		writeSSE(t, w, `data: {"id":"c","object":"chat.completion.chunk","created":1,"model":"gpt-4.1-nano","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`+"\n\n")
		writeSSE(t, w, `data: {"id":"c","object":"chat.completion.chunk","created":1,"model":"gpt-4.1-nano","choices":[],"usage":{"prompt_tokens":5,"completion_tokens":4,"total_tokens":9}}`+"\n\n")
		writeSSE(t, w, "data: [DONE]\n\n")
	})

	record := recordOf(t, twoChoices, streamedChatCall(t, context.Background()))

	assert.Equal(t, "Paris", record["outputResponse"])
}

func TestPromptCapture_StreamStoppedEarlyIsMarkedTruncated(t *testing.T) {
	t.Setenv(core.EnvCapturePrompts, "true")

	record := recordOf(t, sseChatHandler(t), func(r *ReveniumOpenAI) {
		stream, err := r.Chat().Completions().NewStreaming(context.Background(), captureChatParams())
		require.NoError(t, err)
		require.True(t, stream.Next())
		require.NoError(t, stream.Close())
	})

	assert.Equal(t, "hello...[TRUNCATED]", record["outputResponse"])
	assert.Equal(t, true, record["promptsTruncated"])
}

func TestPromptCapture_ResponsesStreamStoppedEarlyIsMarkedTruncated(t *testing.T) {
	t.Setenv(core.EnvCapturePrompts, "true")

	record := recordOf(t, sseResponsesHandler(t, responsesTextDelta, capturedResponsesStreamCompleted), func(r *ReveniumOpenAI) {
		stream, err := r.Responses().CreateStreaming(context.Background(), responsesCaptureParams())
		require.NoError(t, err)
		require.True(t, stream.Next())
		require.NoError(t, stream.Close())
	})

	assert.Equal(t, "Answer in one word.", record["systemPrompt"])
	assert.Equal(t, "Hi...[TRUNCATED]", record["outputResponse"])
	assert.Equal(t, true, record["promptsTruncated"])
}

func TestPromptCapture_StreamClosedAfterItsFinishReasonIsComplete(t *testing.T) {
	t.Setenv(core.EnvCapturePrompts, "true")

	record := recordOf(t, sseChatHandler(t), func(r *ReveniumOpenAI) {
		stream, err := r.Chat().Completions().NewStreaming(context.Background(), captureChatParams())
		require.NoError(t, err)
		for i := 0; i < 3; i++ {
			require.True(t, stream.Next())
		}
		require.NoError(t, stream.Close())
	})

	assert.Equal(t, "hello world", record["outputResponse"])
	assert.NotContains(t, record, "promptsTruncated")
}

func TestStreaming_StopReasonIsChoiceZerosWhereverItSits(t *testing.T) {
	twoChoices := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		writeSSE(t, w, `data: {"id":"c","object":"chat.completion.chunk","created":1,"model":"gpt-4.1-nano","choices":[{"index":1,"delta":{"content":"Lyon"},"finish_reason":"length"},{"index":0,"delta":{"content":"Paris"},"finish_reason":"stop"}]}`+"\n\n")
		writeSSE(t, w, `data: {"id":"c","object":"chat.completion.chunk","created":1,"model":"gpt-4.1-nano","choices":[{"index":1,"delta":{},"finish_reason":"length"}]}`+"\n\n")
		writeSSE(t, w, "data: [DONE]\n\n")
	})

	record := recordOf(t, twoChoices, streamedChatCall(t, context.Background()))

	assert.Equal(t, "END", record["stopReason"])
}

func cutBasicCredential() string {
	credential := base64.StdEncoding.EncodeToString([]byte("admin:" + strings.Repeat("p", 60)))
	return credential[:len(credential)-3]
}

func sseEvents(t *testing.T, events ...string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for _, event := range events {
			writeSSE(t, w, event)
		}
	})
}

func jsonString(text string) string {
	encoded, _ := json.Marshal(text)
	return string(encoded)
}

type chunkedStream struct {
	chunk func(text, finishReason string) string
	read  func(t *testing.T) func(r *ReveniumOpenAI)
}

var streamsEndedByAFinishReason = map[string]chunkedStream{
	"chat completions": {
		chunk: func(text, finishReason string) string {
			return fmt.Sprintf(`data: {"id":"c","object":"chat.completion.chunk","created":1,"model":"gpt-4.1-nano","choices":[{"index":0,"delta":{"content":%s},"finish_reason":%s}]}`+"\n\n", jsonString(text), finishReason)
		},
		read: func(t *testing.T) func(r *ReveniumOpenAI) { return streamedChatCall(t, context.Background()) },
	},
	"legacy completions": {
		chunk: func(text, finishReason string) string {
			return fmt.Sprintf(`data: {"id":"cmpl-1","object":"text_completion","created":1,"model":"gpt-3.5-turbo-instruct","choices":[{"index":0,"text":%s,"finish_reason":%s}]}`+"\n\n", jsonString(text), finishReason)
		},
		read: func(t *testing.T) func(r *ReveniumOpenAI) {
			return func(r *ReveniumOpenAI) {
				stream, err := r.Completions().NewStreaming(context.Background(), textCompletionParams())
				require.NoError(t, err)
				for stream.Next() {
				}
				require.NoError(t, stream.Close())
			}
		},
	},
}

func TestPromptCapture_StreamThatEndsCleanlyBeforeItsFinishReasonIsTruncatedAndMasksACutCredential(t *testing.T) {
	t.Setenv(core.EnvCapturePrompts, "true")
	cut := cutBasicCredential()
	for name, stream := range streamsEndedByAFinishReason {
		t.Run(name, func(t *testing.T) {
			record := recordOf(t, sseEvents(t, stream.chunk("Use Basic "+cut, "null")), stream.read(t))

			assert.Equal(t, true, record["promptsTruncated"])
			reply := record["outputResponse"].(string)
			assert.Contains(t, reply, `Basic ***REDACTED***`)
			assert.NotContains(t, reply, cut[:24])
		})
	}
}

func TestPromptCapture_StreamReadToItsFinishReasonIsComplete(t *testing.T) {
	t.Setenv(core.EnvCapturePrompts, "true")
	for name, stream := range streamsEndedByAFinishReason {
		t.Run(name, func(t *testing.T) {
			record := recordOf(t, sseEvents(t, stream.chunk("Checking.", `"stop"`), "data: [DONE]\n\n"), stream.read(t))

			assert.Equal(t, "Checking.", record["outputResponse"])
			assert.NotContains(t, record, "promptsTruncated")
		})
	}
}

func TestPromptCapture_ResponsesStreamThatEndsCleanlyBeforeItsTerminalEventIsTruncated(t *testing.T) {
	t.Setenv(core.EnvCapturePrompts, "true")
	cut := cutBasicCredential()
	delta := `data: {"type":"response.output_text.delta","item_id":"msg_1","output_index":0,"content_index":0,"delta":` + jsonString("Use Basic "+cut) + `,"sequence_number":1}` + "\n\n"

	record := recordOf(t, sseEvents(t, delta), func(r *ReveniumOpenAI) {
		stream, err := r.Responses().CreateStreaming(context.Background(), responsesCaptureParams())
		require.NoError(t, err)
		for stream.Next() {
		}
		require.NoError(t, stream.Close())
	})

	assert.Equal(t, true, record["promptsTruncated"])
	reply := record["outputResponse"].(string)
	assert.Contains(t, reply, `Basic ***REDACTED***`)
	assert.NotContains(t, reply, cut[:24])
}
