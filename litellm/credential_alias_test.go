package litellm

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

func TestCredentialAliasHeaderReachesTheSubscriberCredentialName(t *testing.T) {
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Revenium-Credential-Alias", "gateway-team-key")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(attributedChatBody))
	}))
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
	record := mock.GetPayloads()[0]
	assert.Equal(t, map[string]interface{}{"credential": map[string]interface{}{"name": "gateway-team-key"}}, record["subscriber"])
	assert.NotContains(t, record, "credentialAlias")
}
