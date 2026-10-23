package litellm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/revenium/revenium-go-sdk/core/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractMetadataFromHeaders_WhitespaceOnlyCountsAsNotSent(t *testing.T) {
	h := http.Header{}
	h.Set("X-Revenium-Organization-Name", "   ")
	h.Set("X-Revenium-Organization-Id", "acme-from-id")
	h.Set("X-Revenium-Product-Name", "\t")
	h.Set("X-Revenium-Product-Id", "prod-from-id")
	h.Set("X-Revenium-Agent", "  ")
	h.Set("X-Revenium-Subscriber-Email", " ")
	h.Set("X-Revenium-Subscriber-Credential", "  ")
	h.Set("X-Revenium-Capture-Prompts", " ")
	h.Set("X-Revenium-Trace-Id", " trace-padded ")

	md := ExtractMetadataFromHeaders(h)

	assert.Equal(t, "acme-from-id", md["organizationName"])
	assert.Equal(t, "prod-from-id", md["productName"])
	assert.NotContains(t, md, "agent")
	assert.NotContains(t, md, "subscriber")
	assert.NotContains(t, md, "capturePrompts")
	assert.Equal(t, " trace-padded ", md["traceId"], "a filled value is sent as received")
}

func TestExtractMetadataFromHeaders_OnlyWhitespaceHeadersGiveNoMetadata(t *testing.T) {
	h := http.Header{}
	h.Set("X-Revenium-Organization-Name", " ")
	h.Set("X-Revenium-Task-Type", "  ")
	assert.Nil(t, ExtractMetadataFromHeaders(h))
}

func TestMergeMetadata_BlankContextValueKeepsTheHeaderValue(t *testing.T) {
	ctxMeta := map[string]interface{}{"organizationName": "  ", "productName": "", "agent": "ctx-agent"}
	headerMeta := map[string]interface{}{"organizationName": "acme", "productName": "prod-a", "agent": "header-agent"}

	merged := MergeMetadata(ctxMeta, headerMeta)

	assert.Equal(t, "acme", merged["organizationName"])
	assert.Equal(t, "prod-a", merged["productName"])
	assert.Equal(t, "ctx-agent", merged["agent"])
}

func TestChatRecordKeepsTheHeaderOrganizationWhenTheContextOneIsBlank(t *testing.T) {
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Revenium-Organization-Name", "acme")
		w.Header().Set("Content-Type", "application/json")
		_, err := w.Write([]byte(attributedChatBody))
		assert.NoError(t, err)
	}))
	defer proxy.Close()
	mock := testutil.NewMockMeteringServer()
	defer mock.Close()
	r := newTestLiteLLM(t, proxy.URL, mock.URL())
	defer r.Close()

	ctx := core.WithUsageMetadata(context.Background(), map[string]interface{}{"organizationName": "  "})
	_, err := r.Chat().Completions().New(ctx, ChatCompletionRequest{
		Model:    "openai/gpt-4.1-nano",
		Messages: []ChatMessage{{Role: "user", Content: "hi"}},
	})
	require.NoError(t, err)

	record := singlePayload(t, r, mock)
	assert.Equal(t, "acme", record["organizationName"])
}
