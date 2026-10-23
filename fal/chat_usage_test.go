package fal

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

const chatEndpoint = "openrouter/router/openai/v1/chat/completions"

func chatReply(finishReason string, usage string) string {
	return fmt.Sprintf(`{"choices":[{"index":0,"finish_reason":%q,"message":{"role":"assistant","content":"hi"}}],"usage":%s}`, finishReason, usage)
}

func TestRun_ChatStopReasonFollowsFinishReason(t *testing.T) {
	cases := map[string]string{
		"stop":           "END",
		"length":         "TOKEN_LIMIT",
		"tool_calls":     "END_SEQUENCE",
		"content_filter": "ERROR",
	}
	for finishReason, want := range cases {
		t.Run(finishReason, func(t *testing.T) {
			record := meterFalCall(t, chatEndpoint, chatReply(finishReason, `{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}`), map[string]interface{}{"prompt": "hi"})

			assert.Equal(t, "CHAT", record["operationType"])
			assert.Equal(t, want, record["stopReason"])
		})
	}
}

func TestRun_ChatRecordsCachedAndReasoningTokens(t *testing.T) {
	usage := `{"prompt_tokens":2048,"completion_tokens":64,"total_tokens":2112,
		"prompt_tokens_details":{"cached_tokens":1024},"completion_tokens_details":{"reasoning_tokens":40}}`

	record := meterFalCall(t, chatEndpoint, chatReply("length", usage), map[string]interface{}{"prompt": "hi"})

	assert.Equal(t, "TOKEN_LIMIT", record["stopReason"])
	assert.EqualValues(t, 1024, record["cacheReadTokenCount"])
	assert.EqualValues(t, 1024, record["inputTokenCount"], "the cached tokens are billed at the cache rate, not again as input")
	assert.EqualValues(t, 64, record["outputTokenCount"])
	assert.EqualValues(t, 40, record["reasoningTokenCount"])
	assert.EqualValues(t, 2112, record["totalTokenCount"])
}

func TestRun_ChatWithoutUsageDetailsIsRecordedAsBefore(t *testing.T) {
	record := meterFalCall(t, chatEndpoint, `{"output":"hi","usage":{"prompt_tokens":12,"completion_tokens":3}}`, map[string]interface{}{"prompt": "hi"})

	assert.Equal(t, "END", record["stopReason"])
	assert.EqualValues(t, 12, record["inputTokenCount"])
	assert.EqualValues(t, 0, record["cacheReadTokenCount"])
	assert.EqualValues(t, 15, record["totalTokenCount"])
}
