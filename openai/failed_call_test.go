package openai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	openaisdk "github.com/openai/openai-go/v3"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/revenium/revenium-go-sdk/core/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const revokedKeyReply = `{"error":{"message":"Incorrect API key provided: sk-proj-********************FAKEWXYZ. You can find your API key at https://platform.openai.com/account/api-keys.","type":"invalid_request_error","param":null,"code":"invalid_api_key"}}`

func errorResponder(status int, body string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	})
}

func failingCalls() map[string]func(r *ReveniumOpenAI) error {
	ctx := context.Background()
	return map[string]func(r *ReveniumOpenAI) error{
		"chat": func(r *ReveniumOpenAI) error {
			_, err := r.Chat().Completions().New(ctx, cachedChatParams())
			return err
		},
		"streamed chat": func(r *ReveniumOpenAI) error {
			stream, err := r.Chat().Completions().NewStreaming(ctx, cachedChatParams())
			if err != nil {
				return err
			}
			for stream.Next() {
			}
			_ = stream.Close()
			return stream.Err()
		},
		"responses": func(r *ReveniumOpenAI) error {
			_, err := r.Responses().Create(ctx, cachedResponsesParams())
			return err
		},
		"embeddings": func(r *ReveniumOpenAI) error {
			_, err := r.Embeddings().Create(ctx, openaisdk.EmbeddingNewParams{
				Model: "text-embedding-3-small",
				Input: openaisdk.EmbeddingNewParamsInputUnion{OfString: openaisdk.String("hi")},
			})
			return err
		},
		"images": func(r *ReveniumOpenAI) error {
			_, err := r.Images().Generate(ctx, openaisdk.ImageGenerateParams{Prompt: "a cat", Model: openaisdk.ImageModelGPTImage1})
			return err
		},
		"legacy completions": func(r *ReveniumOpenAI) error {
			_, err := r.Completions().New(ctx, legacyCompletionParams())
			return err
		},
	}
}

func TestFailedCall_RecordKeepsTheStatusAndErrorCodeButNotTheKeyTail(t *testing.T) {
	t.Setenv("REVENIUM_CAPTURE_ERROR_MESSAGES", "true")
	t.Setenv("REVENIUM_CAPTURE_PROMPTS", "")
	for name, call := range failingCalls() {
		t.Run(name, func(t *testing.T) {
			record := recordedCall(t, errorResponder(http.StatusUnauthorized, revokedKeyReply), func(r *ReveniumOpenAI) error {
				require.Error(t, call(r))
				return nil
			})

			assert.Equal(t, "ERROR", record["stopReason"])
			assert.Equal(t, "invalid_request_error/invalid_api_key (HTTP 401)", record["errorReason"])
			assert.Equal(t, float64(401), record["errorCode"])
			assert.NotContains(t, record["errorReason"], "FAKEWXYZ")
			assert.Nil(t, record["inputMessages"])
		})
	}
}

func TestFailedCall_CredentialsInTheBaseURLStayOutOfTheRecord(t *testing.T) {
	t.Setenv("REVENIUM_CAPTURE_ERROR_MESSAGES", "true")
	t.Setenv("REVENIUM_CAPTURE_PROMPTS", "")
	t.Setenv(core.EnvCapturePrompts, "true")
	providerServer := httptest.NewServer(errorResponder(http.StatusBadRequest, `{"error":{"message":"bad request","type":"invalid_request_error"}}`))
	defer providerServer.Close()
	mock := testutil.NewMockMeteringServer()
	defer mock.Close()
	r := newTestOpenAI(t, strings.Replace(providerServer.URL, "http://", "http://proxy-user:s3cret-pass@", 1), mock.URL())
	defer r.Close()

	_, err := r.Chat().Completions().New(context.Background(), cachedChatParams())
	require.Error(t, err)
	require.Contains(t, err.Error(), "s3cret-pass", "the caller's own error is unchanged")
	r.Flush()

	reason := assertSingleRecordAfterTimeout(t, mock)["errorReason"].(string)
	assert.True(t, strings.HasPrefix(reason, "invalid_request_error: POST"), reason)
	assert.NotContains(t, reason, "s3cret-pass")
	assert.NotContains(t, reason, "proxy-user")
}

func TestFailedResponse_SendsItsRedactedMessageOnlyWhenBothCaptureFlagsAreOn(t *testing.T) {
	failed := `{"id":"resp_f","object":"response","created_at":1700000000,"model":"gpt-4.1-nano","status":"failed","error":{"code":"server_error","message":"The model failed on 'say hello please' for jane@example.com"},"output":[],"usage":{"input_tokens":12,"input_tokens_details":{"cached_tokens":0},"output_tokens":0,"output_tokens_details":{"reasoning_tokens":0},"total_tokens":12}}`
	call := func(r *ReveniumOpenAI) error {
		_, err := r.Responses().Create(context.Background(), cachedResponsesParams())
		return err
	}

	t.Setenv("REVENIUM_CAPTURE_ERROR_MESSAGES", "true")
	t.Setenv("REVENIUM_CAPTURE_PROMPTS", "")
	record := recordedCall(t, jsonResponder(failed), call)
	assert.Equal(t, "server_error", record["errorReason"], "without prompt capture the provider's text stays out")

	t.Setenv("REVENIUM_CAPTURE_PROMPTS", "true")
	record = recordedCall(t, jsonResponder(failed), call)
	assert.Equal(t, "ERROR", record["stopReason"])
	assert.Equal(t, float64(12), record["inputTokenCount"])
	assert.Equal(t, "server_error: The model failed on 'say hello please' for [REDACTED]", record["errorReason"])
}

func TestStreamErrorEvent_IsTheProvidersAnswerAndNeedsPromptCaptureForItsText(t *testing.T) {
	t.Setenv("REVENIUM_CAPTURE_ERROR_MESSAGES", "true")
	t.Setenv("REVENIUM_CAPTURE_PROMPTS", "")
	provider := sseResponder(t,
		`{"id":"chatcmpl-1","object":"chat.completion.chunk","created":1700000000,"model":"gpt-4.1-nano","choices":[{"index":0,"delta":{"role":"assistant","content":"hi"},"finish_reason":null}]}`,
		`{"error":{"message":"The server had an error processing 'say hello please'","type":"server_error","code":"overloaded"}}`,
	)

	record := recordedCall(t, provider, func(r *ReveniumOpenAI) error {
		require.Error(t, failingCalls()["streamed chat"](r))
		return nil
	})

	assert.Equal(t, "ERROR", record["stopReason"])
	assert.Equal(t, "server_error/overloaded", record["errorReason"])
	assert.NotContains(t, record["errorReason"], "say hello please")
}

func TestStreamErrorAfterUsage_KeepsTheCountsOpenAIReported(t *testing.T) {
	provider := sseResponder(t,
		`{"id":"chatcmpl-1","object":"chat.completion.chunk","created":1700000000,"model":"gpt-4.1-nano","choices":[{"index":0,"delta":{"role":"assistant","content":"hi"},"finish_reason":null}]}`,
		`{"id":"chatcmpl-1","object":"chat.completion.chunk","created":1700000000,"model":"gpt-4.1-nano","choices":[],"usage":{"prompt_tokens":37,"completion_tokens":11,"total_tokens":48}}`,
		`{"error":{"message":"server error","type":"server_error","code":"overloaded"}}`,
	)

	record := recordedCall(t, provider, func(r *ReveniumOpenAI) error {
		require.Error(t, failingCalls()["streamed chat"](r))
		return nil
	})

	assert.Equal(t, "ERROR", record["stopReason"])
	assert.Equal(t, float64(37), record["inputTokenCount"])
	assert.Equal(t, float64(11), record["outputTokenCount"])
}
