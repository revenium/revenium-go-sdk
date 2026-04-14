package litellm

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestExtractMetadataFromHeaders_AllFields(t *testing.T) {
	h := http.Header{}
	h.Set("X-Revenium-Subscriber-Id", "sub-1")
	h.Set("X-Revenium-Product-Name", "prod-a")
	h.Set("X-Revenium-Trace-Id", "trace-xyz")
	h.Set("X-Revenium-Retry-Number", "3")
	h.Set("X-Revenium-Capture-Prompts", "true")

	md := ExtractMetadataFromHeaders(h)
	assert.Equal(t, "sub-1", md["subscriberId"])
	assert.Equal(t, "prod-a", md["productName"])
	assert.Equal(t, "trace-xyz", md["traceId"])
	assert.Equal(t, 3, md["retryNumber"])
	assert.Equal(t, true, md["capturePrompts"])
}

func TestExtractMetadataFromHeaders_NilAndEmpty(t *testing.T) {
	assert.Nil(t, ExtractMetadataFromHeaders(nil))
	assert.Nil(t, ExtractMetadataFromHeaders(http.Header{}))
}

func TestExtractMetadataFromHeaders_RetryNumberInvalid(t *testing.T) {
	h := http.Header{}
	h.Set("X-Revenium-Retry-Number", "not-a-number")
	h.Set("X-Revenium-Agent", "agent-x")
	md := ExtractMetadataFromHeaders(h)
	_, hasRetry := md["retryNumber"]
	assert.False(t, hasRetry)
	assert.Equal(t, "agent-x", md["agent"])
}

func TestMergeMetadata_ContextWins(t *testing.T) {
	ctxMeta := map[string]interface{}{"productName": "from-ctx", "traceId": "ctx-trace"}
	headerMeta := map[string]interface{}{"productName": "from-header", "agent": "from-header"}

	merged := MergeMetadata(ctxMeta, headerMeta)
	assert.Equal(t, "from-ctx", merged["productName"])
	assert.Equal(t, "ctx-trace", merged["traceId"])
	assert.Equal(t, "from-header", merged["agent"])
}

func TestMergeMetadata_BothEmpty(t *testing.T) {
	assert.Nil(t, MergeMetadata(nil, nil))
}
