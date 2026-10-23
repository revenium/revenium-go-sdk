package anthropic

import (
	"context"
	"testing"

	anthropicsdk "github.com/anthropics/anthropic-sdk-go"
	"github.com/revenium/revenium-go-sdk/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const usageReportingEveryCount = `"usage":{"input_tokens":37,"output_tokens":11,"cache_creation_input_tokens":13,"cache_read_input_tokens":17,"output_tokens_details":{"thinking_tokens":5}}`

const messageCutAtTokenLimit = `{"id":"msg_contract","type":"message","role":"assistant","model":"claude-haiku-4-5",` +
	`"content":[{"type":"text","text":"cut"}],"stop_reason":"max_tokens",` + usageReportingEveryCount + `}`

func attributionMetadata() map[string]interface{} {
	return map[string]interface{}{
		"organizationName": "org-contract",
		"productName":      "product-contract",
		"agent":            "agent-contract",
		"taskType":         "task-contract",
		"traceId":          "trace-contract",
		"subscriptionId":   "subscription-contract",
	}
}

func attributedContext() context.Context {
	return core.WithUsageMetadata(context.Background(), attributionMetadata())
}

func assertReportedUsageAndTokenLimit(t *testing.T, record map[string]interface{}) {
	t.Helper()
	assert.Equal(t, float64(37), record["inputTokenCount"])
	assert.Equal(t, float64(11), record["outputTokenCount"])
	assert.Equal(t, float64(48), record["totalTokenCount"])
	assert.Equal(t, float64(13), record["cacheCreationTokenCount"])
	assert.Equal(t, float64(17), record["cacheReadTokenCount"])
	assert.Equal(t, float64(5), record["reasoningTokenCount"])
	assert.Equal(t, "TOKEN_LIMIT", record["stopReason"])
	for field, want := range attributionMetadata() {
		assert.Equal(t, want, record[field], "record field %q", field)
	}
}

func TestMessageRecord_CarriesReportedUsageTokenLimitAndAttribution(t *testing.T) {
	newBetaProviderStub(t, messageCutAtTokenLimit)
	r, mock := newMeteredAnthropic(t)

	_, err := r.Messages().CreateMessage(attributedContext(), anthropicsdk.MessageNewParams{
		Model:     "claude-haiku-4-5",
		MaxTokens: 16,
		Messages:  []anthropicsdk.MessageParam{anthropicsdk.NewUserMessage(anthropicsdk.NewTextBlock("hi"))},
	})
	require.NoError(t, err)

	records := recordsAfterFlush(r, mock)
	require.Len(t, records, 1)
	assertReportedUsageAndTokenLimit(t, records[0])
}

func TestBetaMessageRecord_CarriesReportedUsageTokenLimitAndAttribution(t *testing.T) {
	newBetaProviderStub(t, messageCutAtTokenLimit)
	r, mock := newMeteredAnthropic(t)

	_, err := r.BetaMessages().CreateMessage(attributedContext(), betaParams())
	require.NoError(t, err)

	records := recordsAfterFlush(r, mock)
	require.Len(t, records, 1)
	assertReportedUsageAndTokenLimit(t, records[0])
}
