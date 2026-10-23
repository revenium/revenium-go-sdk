package litellm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/revenium/revenium-go-sdk/core/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var attributionHeaders = map[string]string{
	"X-Revenium-Subscriber-Id":              "sub_123",
	"X-Revenium-Subscriber-Email":           "alice@example.com",
	"X-Revenium-Subscriber-Credential":      "cred-alice",
	"X-Revenium-Subscriber-Credential-Name": "alice-key",
	"X-Revenium-Agentic-Job-Id":             "job-42",
	"X-Revenium-Agentic-Job-Name":           "nightly-triage",
	"X-Revenium-Agentic-Job-Type":           "batch",
	"X-Revenium-Agentic-Job-Version":        "v3",
	"X-Revenium-Ticket-Id":                  "TICKET-7",
}

const attributedChatBody = `{"id":"chatcmpl-attr","object":"chat.completion","created":1700000000,"model":"openai/gpt-4.1-nano","choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":2,"total_tokens":12}}`

const attributedStreamBody = "data: {\"id\":\"chatcmpl-attr\",\"model\":\"openai/gpt-4.1-nano\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hi\"}}]}\n\n" +
	"data: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":2,\"total_tokens\":12}}\n\n" +
	"data: [DONE]\n\n"

func newAttributingProxy(t *testing.T, contentType, body string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for name, value := range attributionHeaders {
			w.Header().Set(name, value)
		}
		w.Header().Set("Content-Type", contentType)
		_, err := w.Write([]byte(body))
		assert.NoError(t, err)
	}))
}

var headerSubscriber = map[string]interface{}{
	"id":    "sub_123",
	"email": "alice@example.com",
	"credential": map[string]interface{}{
		"name":  "alice-key",
		"value": "cred-alice",
	},
}

func assertHeaderAttribution(t *testing.T, record map[string]interface{}) {
	t.Helper()
	assert.Equal(t, headerSubscriber, record["subscriber"])
	assert.Equal(t, "job-42", record["agenticJobId"])
	assert.Equal(t, "nightly-triage", record["agenticJobName"])
	assert.Equal(t, "batch", record["agenticJobType"])
	assert.Equal(t, "v3", record["agenticJobVersion"])
	assert.Equal(t, "TICKET-7", record["ticketId"])
}

func TestChatRecordCarriesProxyAttributionHeaders(t *testing.T) {
	proxy := newAttributingProxy(t, "application/json", attributedChatBody)
	defer proxy.Close()
	mock := testutil.NewMockMeteringServer()
	defer mock.Close()

	r := newTestLiteLLM(t, proxy.URL, mock.URL())
	defer r.Close()

	_, err := r.Chat().Completions().New(context.Background(), ChatCompletionRequest{
		Model:    "openai/gpt-4.1-nano",
		Messages: []ChatMessage{{Role: "user", Content: "hi"}},
	})
	require.NoError(t, err)
	r.Flush()

	require.True(t, mock.WaitForPayloads(1, 2*time.Second))
	assertHeaderAttribution(t, mock.GetPayloads()[0])
}

func TestContextSubscriberWinsOverHeaderSubscriberAsAWhole(t *testing.T) {
	cases := []struct {
		name string
		ctx  context.Context
		want map[string]interface{}
	}{
		{
			name: "typed subscriber",
			ctx:  core.WithSubscriber(context.Background(), &core.Subscriber{ID: "ctx-sub", Email: "ctx@example.com"}),
			want: map[string]interface{}{"id": "ctx-sub", "email": "ctx@example.com"},
		},
		{
			name: "id-only typed subscriber does not inherit the header credential",
			ctx:  core.WithSubscriber(context.Background(), &core.Subscriber{ID: "ctx-sub-only"}),
			want: map[string]interface{}{"id": "ctx-sub-only"},
		},
		{
			name: "usage metadata subscriber with its own credential",
			ctx: core.WithUsageMetadata(context.Background(), map[string]interface{}{
				"subscriber": map[string]interface{}{
					"id":         "meta-sub",
					"email":      "meta@example.com",
					"credential": map[string]interface{}{"name": "meta-key", "value": "meta-cred"},
				},
			}),
			want: map[string]interface{}{
				"id":         "meta-sub",
				"email":      "meta@example.com",
				"credential": map[string]interface{}{"name": "meta-key", "value": "meta-cred"},
			},
		},
		{
			name: "id-only usage metadata subscriber does not inherit the header credential",
			ctx: core.WithUsageMetadata(context.Background(), map[string]interface{}{
				"subscriber": map[string]interface{}{"id": "meta-sub-only"},
			}),
			want: map[string]interface{}{"id": "meta-sub-only"},
		},
		{
			name: "usage metadata without a subscriber falls back to the headers",
			ctx:  core.WithUsageMetadata(context.Background(), map[string]interface{}{"productName": "ctx-product"}),
			want: headerSubscriber,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			proxy := newAttributingProxy(t, "application/json", attributedChatBody)
			defer proxy.Close()
			mock := testutil.NewMockMeteringServer()
			defer mock.Close()

			r := newTestLiteLLM(t, proxy.URL, mock.URL())
			defer r.Close()

			_, err := r.Chat().Completions().New(tc.ctx, ChatCompletionRequest{
				Model:    "openai/gpt-4.1-nano",
				Messages: []ChatMessage{{Role: "user", Content: "hi"}},
			})
			require.NoError(t, err)
			r.Flush()

			require.True(t, mock.WaitForPayloads(1, 2*time.Second))
			assert.Equal(t, tc.want, mock.GetPayloads()[0]["subscriber"])
		})
	}
}

func TestStreamingRecordCarriesProxyAttributionHeaders(t *testing.T) {
	proxy := newAttributingProxy(t, "text/event-stream", attributedStreamBody)
	defer proxy.Close()
	mock := testutil.NewMockMeteringServer()
	defer mock.Close()

	r := newTestLiteLLM(t, proxy.URL, mock.URL())
	defer r.Close()

	stream, err := r.Chat().Completions().NewStreaming(context.Background(), ChatCompletionRequest{
		Model:    "openai/gpt-4.1-nano",
		Messages: []ChatMessage{{Role: "user", Content: "hi"}},
	})
	require.NoError(t, err)
	for stream.Next() {
	}
	require.NoError(t, stream.Err())
	require.NoError(t, stream.Close())
	r.Flush()

	require.True(t, mock.WaitForPayloads(1, 2*time.Second))
	assertHeaderAttribution(t, mock.GetPayloads()[0])
}

func TestSubscriberHeadersWithoutCredentialOmitCredential(t *testing.T) {
	h := http.Header{}
	h.Set("X-Revenium-Subscriber-Email", "alice@example.com")

	md := ExtractMetadataFromHeaders(h)

	assert.Equal(t, map[string]interface{}{"email": "alice@example.com"}, md["subscriber"])
}

func TestCredentialHeaderAloneStillReachesSubscriber(t *testing.T) {
	h := http.Header{}
	h.Set("X-Revenium-Subscriber-Credential", "cred-alice")

	md := ExtractMetadataFromHeaders(h)

	assert.Equal(t, map[string]interface{}{
		"credential": map[string]interface{}{"value": "cred-alice"},
	}, md["subscriber"])
}

func TestNoSubscriberHeadersLeaveSubscriberUnset(t *testing.T) {
	h := http.Header{}
	h.Set("X-Revenium-Ticket-Id", "TICKET-7")

	md := ExtractMetadataFromHeaders(h)

	_, hasSubscriber := md["subscriber"]
	assert.False(t, hasSubscriber)
	assert.Equal(t, "TICKET-7", md["ticketId"])
}
