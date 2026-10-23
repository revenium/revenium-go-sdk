package openai

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	openaisdk "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"

	"github.com/revenium/revenium-go-sdk/core/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	cacheWritePromptDetails = `{"cached_tokens":500,"cache_write_tokens":2048}`
	plainPromptDetails      = `{"cached_tokens":500}`
)

type cacheWriteCase struct {
	name             string
	promptDetails    string
	wantInput        float64
	wantCacheRead    float64
	wantCacheWritten float64
}

func cacheWriteCases() []cacheWriteCase {
	return []cacheWriteCase{
		{name: "reply reports a cache write", promptDetails: cacheWritePromptDetails, wantInput: 952, wantCacheRead: 500, wantCacheWritten: 2048},
		{name: "reply without cache_write_tokens", promptDetails: plainPromptDetails, wantInput: 3000, wantCacheRead: 500, wantCacheWritten: 0},
	}
}

func chatUsageJSON(promptDetails string) string {
	return fmt.Sprintf(`{"prompt_tokens":3000,"completion_tokens":20,"total_tokens":3020,"prompt_tokens_details":%s}`, promptDetails)
}

func responsesUsageJSON(promptDetails string) string {
	return fmt.Sprintf(`{"input_tokens":3000,"input_tokens_details":%s,"output_tokens":20,"output_tokens_details":{"reasoning_tokens":0},"total_tokens":3020}`, promptDetails)
}

func jsonResponder(body string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	})
}

func sseResponder(t *testing.T, events ...string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		for _, event := range events {
			writeSSE(t, w, "data: "+event+"\n\n")
		}
		writeSSE(t, w, "data: [DONE]\n\n")
	})
}

func recordedCall(t *testing.T, provider http.Handler, call func(r *ReveniumOpenAI) error) map[string]interface{} {
	t.Helper()
	providerServer := httptest.NewServer(provider)
	defer providerServer.Close()
	mock := testutil.NewMockMeteringServer()
	defer mock.Close()
	r := newTestOpenAI(t, providerServer.URL, mock.URL())
	defer r.Close()

	require.NoError(t, call(r))
	r.Flush()
	return assertSingleRecordAfterTimeout(t, mock)
}

func assertCacheSplit(t *testing.T, tc cacheWriteCase, record map[string]interface{}) {
	t.Helper()
	assert.Equal(t, tc.wantInput, record["inputTokenCount"])
	assert.Equal(t, tc.wantCacheRead, record["cacheReadTokenCount"])
	assert.Equal(t, tc.wantCacheWritten, record["cacheCreationTokenCount"])
	assert.Equal(t, float64(20), record["outputTokenCount"])
	assert.Equal(t, float64(3020), record["totalTokenCount"])
}

func cachedChatParams() openaisdk.ChatCompletionNewParams {
	return openaisdk.ChatCompletionNewParams{
		Model:    "gpt-5.6",
		Messages: []openaisdk.ChatCompletionMessageParamUnion{openaisdk.UserMessage("hi")},
	}
}

func TestChatCompletion_RecordsOpenAICacheWrites(t *testing.T) {
	for _, tc := range cacheWriteCases() {
		t.Run(tc.name, func(t *testing.T) {
			body := `{"id":"chatcmpl-1","object":"chat.completion","created":1700000000,"model":"gpt-5.6",` +
				`"choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}],` +
				`"usage":` + chatUsageJSON(tc.promptDetails) + `}`

			record := recordedCall(t, jsonResponder(body), func(r *ReveniumOpenAI) error {
				_, err := r.Chat().Completions().New(context.Background(), cachedChatParams())
				return err
			})

			assertCacheSplit(t, tc, record)
		})
	}
}

func TestChatCompletionStreaming_RecordsOpenAICacheWrites(t *testing.T) {
	for _, tc := range cacheWriteCases() {
		t.Run(tc.name, func(t *testing.T) {
			provider := sseResponder(t,
				`{"id":"chatcmpl-1","object":"chat.completion.chunk","created":1700000000,"model":"gpt-5.6","choices":[{"index":0,"delta":{"role":"assistant","content":"hi"},"finish_reason":"stop"}]}`,
				`{"id":"chatcmpl-1","object":"chat.completion.chunk","created":1700000000,"model":"gpt-5.6","choices":[],"usage":`+chatUsageJSON(tc.promptDetails)+`}`,
			)

			record := recordedCall(t, provider, func(r *ReveniumOpenAI) error {
				stream, err := r.Chat().Completions().NewStreaming(context.Background(), cachedChatParams())
				if err != nil {
					return err
				}
				for stream.Next() {
				}
				return stream.Close()
			})

			assertCacheSplit(t, tc, record)
		})
	}
}

func cachedResponsesParams() responses.ResponseNewParams {
	return responses.ResponseNewParams{
		Model: "gpt-5.6",
		Input: responses.ResponseNewParamsInputUnion{OfString: openaisdk.String("hi")},
	}
}

func responseJSON(promptDetails string) string {
	return `{"id":"resp_1","object":"response","created_at":1700000000,"model":"gpt-5.6","status":"completed","output":[],"usage":` +
		responsesUsageJSON(promptDetails) + `}`
}

func TestResponses_RecordsOpenAICacheWrites(t *testing.T) {
	for _, tc := range cacheWriteCases() {
		t.Run(tc.name, func(t *testing.T) {
			record := recordedCall(t, jsonResponder(responseJSON(tc.promptDetails)), func(r *ReveniumOpenAI) error {
				_, err := r.Responses().Create(context.Background(), cachedResponsesParams())
				return err
			})

			assertCacheSplit(t, tc, record)
		})
	}
}

func TestResponsesStreaming_RecordsOpenAICacheWrites(t *testing.T) {
	for _, tc := range cacheWriteCases() {
		t.Run(tc.name, func(t *testing.T) {
			provider := sseResponder(t,
				responsesTextDelta,
				`{"type":"response.completed","sequence_number":2,"response":`+responseJSON(tc.promptDetails)+`}`,
			)

			record := recordedCall(t, provider, func(r *ReveniumOpenAI) error {
				stream, err := r.Responses().CreateStreaming(context.Background(), cachedResponsesParams())
				if err != nil {
					return err
				}
				for stream.Next() {
				}
				return stream.Close()
			})

			assertCacheSplit(t, tc, record)
		})
	}
}

func TestCacheWriteLargerThanThePrompt_NeverRecordsNegativeInput(t *testing.T) {
	body := `{"id":"chatcmpl-1","object":"chat.completion","created":1700000000,"model":"gpt-5.6",` +
		`"choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}],` +
		`"usage":{"prompt_tokens":100,"completion_tokens":20,"total_tokens":120,"prompt_tokens_details":{"cache_write_tokens":2048}}}`

	record := recordedCall(t, jsonResponder(body), func(r *ReveniumOpenAI) error {
		_, err := r.Chat().Completions().New(context.Background(), cachedChatParams())
		return err
	})

	assert.Equal(t, float64(0), record["inputTokenCount"])
	assert.Equal(t, float64(2048), record["cacheCreationTokenCount"])
}

func legacyCompletionParams() openaisdk.CompletionNewParams {
	return openaisdk.CompletionNewParams{
		Model:  "gpt-3.5-turbo-instruct",
		Prompt: openaisdk.CompletionNewParamsPromptUnion{OfString: openaisdk.String("hi")},
	}
}

func TestLegacyCompletion_RecordsOpenAICacheWrites(t *testing.T) {
	for _, tc := range cacheWriteCases() {
		t.Run(tc.name, func(t *testing.T) {
			body := `{"id":"cmpl-1","object":"text_completion","created":1700000000,"model":"gpt-3.5-turbo-instruct",` +
				`"choices":[{"index":0,"text":"hi","finish_reason":"stop","logprobs":null}],` +
				`"usage":` + chatUsageJSON(tc.promptDetails) + `}`

			record := recordedCall(t, jsonResponder(body), func(r *ReveniumOpenAI) error {
				_, err := r.Completions().New(context.Background(), legacyCompletionParams())
				return err
			})

			assertCacheSplit(t, tc, record)
		})
	}
}

func TestLegacyCompletionStreaming_RecordsOpenAICacheWrites(t *testing.T) {
	for _, tc := range cacheWriteCases() {
		t.Run(tc.name, func(t *testing.T) {
			provider := sseResponder(t,
				`{"id":"cmpl-1","object":"text_completion","created":1700000000,"model":"gpt-3.5-turbo-instruct","choices":[{"index":0,"text":"hi","finish_reason":"stop"}]}`,
				`{"id":"cmpl-1","object":"text_completion","created":1700000000,"model":"gpt-3.5-turbo-instruct","choices":[],"usage":`+chatUsageJSON(tc.promptDetails)+`}`,
			)

			record := recordedCall(t, provider, func(r *ReveniumOpenAI) error {
				stream, err := r.Completions().NewStreaming(context.Background(), legacyCompletionParams())
				if err != nil {
					return err
				}
				for stream.Next() {
				}
				return stream.Close()
			})

			assertCacheSplit(t, tc, record)
		})
	}
}
