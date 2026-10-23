package perplexity

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFailedChatRecord_KeepsTheErrorTypeAndStatusButNotTheReplyText(t *testing.T) {
	t.Setenv("REVENIUM_CAPTURE_ERROR_MESSAGES", "true")
	t.Setenv("REVENIUM_CAPTURE_PROMPTS", "")
	provider := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"Invalid API key pplx-****************WXYZ for prompt 'say hello please'","type":"authentication_error","code":401}}`))
	})

	record := onlyRecord(t, provider, func(ctx context.Context, r *ReveniumPerplexity) {
		_, err := r.Chat().Completions().New(ctx, sonarParams())
		require.Error(t, err)
	})

	assert.Equal(t, "ERROR", record["stopReason"])
	assert.Equal(t, "authentication_error (HTTP 401)", record["errorReason"])
	assert.Equal(t, float64(401), record["errorCode"])
}

func TestStreamErrorEvent_IsTheProvidersAnswerAndNeedsPromptCaptureForItsText(t *testing.T) {
	t.Setenv("REVENIUM_CAPTURE_ERROR_MESSAGES", "true")
	t.Setenv("REVENIUM_CAPTURE_PROMPTS", "")
	provider := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"error\":{\"message\":\"failed on 'say hello please'\",\"type\":\"server_error\",\"code\":\"overloaded\"}}\n\n"))
	})

	record := onlyRecord(t, provider, func(ctx context.Context, r *ReveniumPerplexity) {
		stream, err := r.Chat().Completions().NewStreaming(ctx, sonarParams())
		require.NoError(t, err)
		for stream.Next() {
		}
		require.Error(t, stream.Err())
		_ = stream.Close()
	})

	assert.Equal(t, "ERROR", record["stopReason"])
	assert.Equal(t, "server_error/overloaded", record["errorReason"])
}

func TestStreamErrorAfterUsage_KeepsTheCountsPerplexityReported(t *testing.T) {
	provider := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"id\":\"p1\",\"object\":\"chat.completion.chunk\",\"created\":1700000000,\"model\":\"sonar\",\"choices\":[],\"usage\":{\"prompt_tokens\":15,\"completion_tokens\":4,\"total_tokens\":19}}\n\n" +
			"data: {\"error\":{\"message\":\"failed\",\"type\":\"server_error\",\"code\":\"overloaded\"}}\n\n"))
	})

	record := onlyRecord(t, provider, func(ctx context.Context, r *ReveniumPerplexity) {
		stream, err := r.Chat().Completions().NewStreaming(ctx, sonarParams())
		require.NoError(t, err)
		for stream.Next() {
			_ = stream.Current()
		}
		require.Error(t, stream.Err())
		_ = stream.Close()
	})

	assert.Equal(t, "ERROR", record["stopReason"])
	assert.Equal(t, float64(15), record["inputTokenCount"])
	assert.Equal(t, float64(4), record["outputTokenCount"])
}
