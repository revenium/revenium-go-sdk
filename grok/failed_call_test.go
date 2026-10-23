package grok

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/revenium/revenium-go-sdk/core/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProviderError_IsRecordedOnceAsAnError(t *testing.T) {
	cases := map[string]struct {
		status int
		body   string
		reason string
	}{
		"rate limited": {http.StatusTooManyRequests, `{"code":"Some resource has been exhausted","error":"rate limited"}`, "PROVIDER_ERROR (HTTP 429)"},
		"bad request":  {http.StatusBadRequest, `{"error":"say hello please is not a valid request"}`, "PROVIDER_ERROR (HTTP 400)"},
		"server error": {http.StatusInternalServerError, `upstream connect error`, "PROVIDER_ERROR (HTTP 500)"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer provider.Close()
			mock := testutil.NewMockMeteringServer()
			defer mock.Close()
			r := newTestGrok(t, provider.URL, mock.URL())
			defer r.Close()

			_, err := r.ChatCompletions(context.Background(), ChatCompletionRequest{Model: "grok-4", Messages: []ChatMessage{{Role: "user", Content: "say hello please"}}})
			require.Error(t, err)
			r.Flush()

			require.True(t, mock.WaitForPayloads(1, 2*time.Second))
			payloads := mock.GetPayloads()
			require.Len(t, payloads, 1)
			assert.Equal(t, "ERROR", payloads[0]["stopReason"])
			assert.Equal(t, tc.reason, payloads[0]["errorReason"])
			assert.Equal(t, float64(tc.status), payloads[0]["errorCode"])
			assert.Equal(t, "grok-4", payloads[0]["model"])
			assert.Equal(t, float64(0), payloads[0]["inputTokenCount"])
		})
	}
}

func TestUnreachableProvider_IsRecordedOnceAsANetworkError(t *testing.T) {
	provider := httptest.NewServer(http.NotFoundHandler())
	provider.Close()
	mock := testutil.NewMockMeteringServer()
	defer mock.Close()
	r := newTestGrok(t, provider.URL, mock.URL())
	defer r.Close()

	_, err := r.ChatCompletions(context.Background(), ChatCompletionRequest{Model: "grok-4", Messages: []ChatMessage{{Role: "user", Content: "hi"}}})
	require.Error(t, err)
	r.Flush()

	require.True(t, mock.WaitForPayloads(1, 2*time.Second))
	payload := mock.GetPayloads()[0]
	assert.Equal(t, "ERROR", payload["stopReason"])
	assert.Equal(t, "NETWORK_ERROR", payload["errorReason"])
	assert.Nil(t, payload["errorCode"])
}

func TestFailedVisionCall_IsRecordedUnderTheVisionOperation(t *testing.T) {
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"image too large"}`))
	}))
	defer provider.Close()
	mock := testutil.NewMockMeteringServer()
	defer mock.Close()
	r := newTestGrok(t, provider.URL, mock.URL())
	defer r.Close()
	content := []interface{}{map[string]interface{}{"type": "image_url", "image_url": map[string]interface{}{"url": "https://x/cat.png"}}}

	_, err := r.ChatCompletions(context.Background(), ChatCompletionRequest{Model: "grok-4", Messages: []ChatMessage{{Role: "user", Content: content}}})
	require.Error(t, err)
	r.Flush()

	require.True(t, mock.WaitForPayloads(1, 2*time.Second))
	assert.Equal(t, "VISION", mock.GetPayloads()[0]["operationType"], "the same operation as a successful call with the same content")
}
