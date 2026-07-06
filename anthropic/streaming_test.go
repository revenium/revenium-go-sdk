package anthropic

import (
	"testing"
	"time"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/revenium/revenium-go-sdk/core/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestAnthropic(t *testing.T, meteringURL string) *ReveniumAnthropic {
	t.Helper()
	cfg := &Config{
		AnthropicAPIKey: "sk-ant-test",
		Revenium: &core.ReveniumConfig{
			APIKey:  "hak_test_key_123",
			BaseURL: meteringURL,
		},
	}
	r, err := NewReveniumAnthropic(cfg)
	require.NoError(t, err)
	return r
}

func TestStreamingWrapper_SetInputTokensUpdatesTotal(t *testing.T) {
	w := &StreamingWrapper{outputTokens: 7}
	w.SetInputTokens(10)
	in, out, total := w.GetTokenCounts()
	assert.Equal(t, 10, in)
	assert.Equal(t, 7, out)
	assert.Equal(t, 17, total)
}

func TestStreamingWrapper_SetModel(t *testing.T) {
	w := &StreamingWrapper{model: "claude-haiku-4-5"}
	w.SetModel("claude-sonnet-4-6")
	assert.Equal(t, "claude-sonnet-4-6", w.model)
}

func TestStreamingWrapper_CloseSendsMeteringPayload(t *testing.T) {
	mock := testutil.NewMockMeteringServer()
	defer mock.Close()

	r := newTestAnthropic(t, mock.URL())
	defer r.Close()

	startTime := time.Now().Add(-150 * time.Millisecond)
	now := startTime.Add(50 * time.Millisecond)
	w := &StreamingWrapper{
		config:               r.config,
		metadata:             map[string]interface{}{"traceId": "trace-x"},
		startTime:            startTime,
		firstTokenTime:       &now,
		messagesAPI:          &MessagesInterface{parent: r},
		model:                "claude-haiku-4-5",
		provider:             "anthropic",
		stopReason:           "end_turn",
		inputTokens:          120,
		outputTokens:         45,
		totalTokens:          165,
		cacheCreationTokens:  3,
		cacheReadTokens:      2,
		hasVision:            true,
		accumulatedTextParts: []string{"hello", " world"},
	}

	require.NoError(t, w.Close())
	r.Flush()

	require.True(t, mock.WaitForPayloads(1, time.Second))
	payloads := mock.GetPayloads()
	require.Len(t, payloads, 1)
	p := payloads[0]
	assert.Equal(t, "CHAT", p["operationType"])
	assert.Equal(t, "claude-haiku-4-5", p["model"])
	assert.Equal(t, true, p["isStreamed"])
	assert.Equal(t, float64(120), p["inputTokenCount"])
	assert.Equal(t, float64(45), p["outputTokenCount"])
	assert.Equal(t, float64(165), p["totalTokenCount"])
	assert.Equal(t, "END", p["stopReason"])
	assert.Equal(t, "trace-x", p["traceId"])
	attrs, _ := p["attributes"].(map[string]interface{})
	require.NotNil(t, attrs)
	assert.Equal(t, true, attrs["hasVision"])
}

func TestReconstructResponseFromChunks(t *testing.T) {
	w := &StreamingWrapper{
		model:                "claude-haiku-4-5",
		stopReason:           "end_turn",
		inputTokens:          10,
		outputTokens:         5,
		cacheCreationTokens:  1,
		cacheReadTokens:      2,
		accumulatedTextParts: []string{"part1 ", "part2"},
	}
	msg := ReconstructResponseFromChunks(w)
	require.NotNil(t, msg)
	assert.Equal(t, "claude-haiku-4-5", string(msg.Model))
	assert.Equal(t, "end_turn", string(msg.StopReason))
	assert.Equal(t, int64(10), msg.Usage.InputTokens)
	assert.Equal(t, int64(5), msg.Usage.OutputTokens)
	assert.Equal(t, int64(1), msg.Usage.CacheCreationInputTokens)
	assert.Equal(t, int64(2), msg.Usage.CacheReadInputTokens)
	require.Len(t, msg.Content, 1)
}

func TestReconstructResponseFromChunks_EmptyContent(t *testing.T) {
	w := &StreamingWrapper{model: "claude-haiku-4-5", stopReason: "end_turn"}
	msg := ReconstructResponseFromChunks(w)
	require.NotNil(t, msg)
	assert.Empty(t, msg.Content)
}
