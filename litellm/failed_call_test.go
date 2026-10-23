package litellm

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/revenium/revenium-go-sdk/core/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var proxyRejections = map[string]struct {
	status int
	body   string
	reason string
}{
	"masked openai key":  {http.StatusUnauthorized, `{"error":{"message":"Incorrect API key provided: sk-proj-********************FAKEWXYZ. You can find your API key at https://platform.openai.com/account/api-keys.","type":"invalid_request_error","param":null,"code":"invalid_api_key"}}`, "invalid_request_error/invalid_api_key (HTTP 401)"},
	"proxy key and hash": {http.StatusUnauthorized, `{"error":{"message":"Authentication Error, Invalid proxy server token passed. Received API Key = sk-...FAKE1234, Key Hash (Token) =0f1e2d3c4b5a69788796a5b4c3d2e1f00f1e2d3c4b5a69788796a5b4c3d2e1f0. Unable to find token in cache or LiteLLM_VerificationTokenTable","type":"auth_error","param":"None","code":"401"}}`, "auth_error (HTTP 401)"},
	"echoed request":     {http.StatusBadRequest, `{"error":{"message":"litellm.BadRequestError: Invalid request body: {'model': 'gpt-4.1-nano', 'messages': [{'role': 'user', 'content': 'say hello please'}], 'temperatur': 2}","type":"invalid_request_error","param":null,"code":"400"}}`, "invalid_request_error (HTTP 400)"},
}

var sensitiveReplyParts = []string{"FAKEWXYZ", "sk-proj", "FAKE1234", "0f1e2d3c4b5a6978", "say hello please"}

func rejectingProxy(status int, body string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
}

func onlyRecord(t *testing.T, r *ReveniumLiteLLM, mock *testutil.MockMeteringServer) map[string]interface{} {
	t.Helper()
	r.Flush()
	require.True(t, mock.WaitForPayloads(1, 2*time.Second), "the failed call must be recorded")
	payloads := mock.GetPayloads()
	require.Len(t, payloads, 1, "one failed call, one record")
	return payloads[0]
}

func helloRequest() ChatCompletionRequest {
	return ChatCompletionRequest{Model: "openai/gpt-4.1-nano", Messages: []ChatMessage{{Role: "user", Content: "say hello please"}}}
}

func TestProxyRejection_RecordHoldsNoKeyFragmentHashOrPromptText(t *testing.T) {
	t.Setenv("REVENIUM_CAPTURE_ERROR_MESSAGES", "true")
	t.Setenv("REVENIUM_CAPTURE_PROMPTS", "")
	for name, tc := range proxyRejections {
		t.Run(name, func(t *testing.T) {
			proxy := rejectingProxy(tc.status, tc.body)
			defer proxy.Close()
			mock := testutil.NewMockMeteringServer()
			defer mock.Close()
			r := newTestLiteLLM(t, proxy.URL, mock.URL())
			defer r.Close()

			_, err := r.Chat().Completions().New(context.Background(), helloRequest())
			require.Error(t, err)

			record := onlyRecord(t, r, mock)
			assert.Equal(t, "ERROR", record["stopReason"])
			assert.Equal(t, tc.reason, record["errorReason"])
			assert.Equal(t, float64(tc.status), record["errorCode"])
			for _, part := range sensitiveReplyParts {
				assert.NotContains(t, record["errorReason"], part)
			}
		})
	}
}

func TestRejectedStream_IsRecordedOnceAsAnErrorLikeTheUnstreamedCall(t *testing.T) {
	for _, status := range []int{http.StatusTooManyRequests, http.StatusInternalServerError} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			proxy := rejectingProxy(status, `{"error":{"message":"busy","type":"proxy_error","code":"`+http.StatusText(status)+`"}}`)
			defer proxy.Close()
			mock := testutil.NewMockMeteringServer()
			defer mock.Close()
			r := newTestLiteLLM(t, proxy.URL, mock.URL())
			defer r.Close()

			_, err := r.Chat().Completions().NewStreaming(context.Background(), helloRequest())
			require.Error(t, err)

			record := onlyRecord(t, r, mock)
			assert.Equal(t, "ERROR", record["stopReason"])
			assert.Equal(t, true, record["isStreamed"])
			assert.Equal(t, float64(status), record["errorCode"])
			assert.Equal(t, "gpt-4.1-nano", record["model"])
		})
	}
}

func TestStreamThatCannotReachTheProxy_IsRecordedOnceAsAnError(t *testing.T) {
	proxy := httptest.NewServer(http.NotFoundHandler())
	proxy.Close()
	mock := testutil.NewMockMeteringServer()
	defer mock.Close()
	r := newTestLiteLLM(t, proxy.URL, mock.URL())
	defer r.Close()

	_, err := r.Chat().Completions().NewStreaming(context.Background(), helloRequest())
	require.Error(t, err)

	record := onlyRecord(t, r, mock)
	assert.Equal(t, "ERROR", record["stopReason"])
	assert.Equal(t, "NETWORK_ERROR", record["errorReason"])
	assert.Nil(t, record["errorCode"])
}

func TestRejectedEmbedding_IsRecordedOnceAsAnError(t *testing.T) {
	proxy := rejectingProxy(http.StatusBadRequest, `{"error":{"message":"bad input","type":"invalid_request_error","code":"400"}}`)
	defer proxy.Close()
	mock := testutil.NewMockMeteringServer()
	defer mock.Close()
	r := newTestLiteLLM(t, proxy.URL, mock.URL())
	defer r.Close()

	_, err := r.Embeddings().New(context.Background(), EmbeddingRequest{Model: "openai/text-embedding-3-small", Input: "hi"})
	require.Error(t, err)

	record := onlyRecord(t, r, mock)
	assert.Equal(t, "EMBED", record["operationType"])
	assert.Equal(t, "invalid_request_error (HTTP 400)", record["errorReason"])
}

func TestStreamErrorEvent_IsRecordedAsAnErrorWithTheProvidersTypeAndCode(t *testing.T) {
	t.Setenv("REVENIUM_CAPTURE_ERROR_MESSAGES", "true")
	t.Setenv("REVENIUM_CAPTURE_PROMPTS", "")
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"id\":\"c1\",\"model\":\"openai/gpt-4.1-nano\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hi\"}}]}\n\n" +
			"data: {\"error\":{\"message\":\"upstream failed on 'say hello please'\",\"type\":\"server_error\",\"code\":\"overloaded\"}}\n\n"))
	}))
	defer proxy.Close()
	mock := testutil.NewMockMeteringServer()
	defer mock.Close()
	r := newTestLiteLLM(t, proxy.URL, mock.URL())
	defer r.Close()

	stream, err := r.Chat().Completions().NewStreaming(context.Background(), helloRequest())
	require.NoError(t, err)
	for stream.Next() {
	}
	require.Error(t, stream.Err(), "the caller learns the stream failed")
	require.NoError(t, stream.Close())

	record := onlyRecord(t, r, mock)
	assert.Equal(t, "ERROR", record["stopReason"])
	assert.Equal(t, "server_error/overloaded", record["errorReason"], "the provider's text needs prompt capture as well")
	assert.Equal(t, true, record["isStreamed"])
	assert.Equal(t, float64(0), record["inputTokenCount"], "no usage was reported before the error")
}

func TestStreamErrorAfterUsage_KeepsTheCountsTheProxyReported(t *testing.T) {
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"id\":\"c1\",\"model\":\"openai/gpt-4.1-nano\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hi\"}}]}\n\n" +
			"data: {\"choices\":[],\"usage\":{\"prompt_tokens\":21,\"completion_tokens\":3,\"total_tokens\":24,\"completion_tokens_details\":{\"reasoning_tokens\":2}}}\n\n" +
			"data: {\"error\":{\"message\":\"upstream failed\",\"type\":\"server_error\",\"code\":\"overloaded\"}}\n\n"))
	}))
	defer proxy.Close()
	mock := testutil.NewMockMeteringServer()
	defer mock.Close()
	r := newTestLiteLLM(t, proxy.URL, mock.URL())
	defer r.Close()

	stream, err := r.Chat().Completions().NewStreaming(context.Background(), helloRequest())
	require.NoError(t, err)
	for stream.Next() {
	}
	require.Error(t, stream.Err())
	require.NoError(t, stream.Close())

	record := onlyRecord(t, r, mock)
	assert.Equal(t, "ERROR", record["stopReason"])
	assert.Equal(t, float64(21), record["inputTokenCount"], "the proxy billed what it reported before failing")
	assert.Equal(t, float64(3), record["outputTokenCount"])
	assert.Equal(t, float64(2), record["reasoningTokenCount"])
}

// Failed-call records never carry captured fields, by design. Capture switched on, by the
// environment or by a proxy header, must not change that.
func TestRejectedCall_RecordCarriesNoCapturedPromptOrReplyWhateverTurnsCaptureOn(t *testing.T) {
	t.Setenv("REVENIUM_CAPTURE_ERROR_MESSAGES", "")
	switches := map[string]struct {
		env    string
		header string
	}{
		"capture on in the environment": {env: "true"},
		"capture on in a proxy header":  {header: "true"},
	}
	for name, on := range switches {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s streamed=%v", name, stream), func(t *testing.T) {
				t.Setenv("REVENIUM_CAPTURE_PROMPTS", on.env)
				record := rejectedCallRecord(t, on.header, stream)

				assert.Equal(t, "ERROR", record["stopReason"])
				assert.Equal(t, float64(http.StatusBadRequest), record["errorCode"])
				for _, field := range []string{"inputMessages", "outputResponse", "systemPrompt"} {
					assert.NotContains(t, record, field)
				}
			})
		}
	}
}

func rejectedCallRecord(t *testing.T, captureHeader string, stream bool) map[string]interface{} {
	t.Helper()
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if captureHeader != "" {
			w.Header().Set("X-Revenium-Capture-Prompts", captureHeader)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"bad request for 'say hello please'","type":"invalid_request_error","code":"400"}}`))
	}))
	defer proxy.Close()
	mock := testutil.NewMockMeteringServer()
	defer mock.Close()
	r := newTestLiteLLM(t, proxy.URL, mock.URL())
	defer r.Close()

	var err error
	if stream {
		_, err = r.Chat().Completions().NewStreaming(context.Background(), helloRequest())
	} else {
		_, err = r.Chat().Completions().New(context.Background(), helloRequest())
	}
	require.Error(t, err)
	return onlyRecord(t, r, mock)
}
