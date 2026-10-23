package google

import (
	"context"
	"net/http"
	"testing"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/genai"
)

const (
	geminiTextChunk         = `{"candidates":[{"content":{"parts":[{"text":"Once upon a time"}],"role":"model"},"index":0}],"usageMetadata":{"promptTokenCount":9,"candidatesTokenCount":4,"totalTokenCount":13},"modelVersion":"gemini-3.5-flash-lite"}`
	geminiMaxTokensChunk    = `{"candidates":[{"content":{"parts":[{"text":" a fox"}],"role":"model"},"finishReason":"MAX_TOKENS","index":0}],"usageMetadata":{"promptTokenCount":9,"candidatesTokenCount":16,"totalTokenCount":25},"modelVersion":"gemini-3.5-flash-lite"}`
	geminiSafetyChunk       = `{"candidates":[{"finishReason":"SAFETY","index":0}],"usageMetadata":{"promptTokenCount":9,"candidatesTokenCount":4,"totalTokenCount":13},"modelVersion":"gemini-3.5-flash-lite"}`
	geminiContinuationChunk = `{"candidates":[{"content":{"parts":[{"text":" a fox"}],"role":"model"},"finishReason":"CONTINUATION","index":0}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":20,"totalTokenCount":30},"modelVersion":"gemini-3.5-flash-lite"}`
	geminiStopChunk         = `{"candidates":[{"finishReason":"STOP","index":0}],"modelVersion":"gemini-3.5-flash-lite"}`
	geminiUsageOnlyChunk    = `{"usageMetadata":{"promptTokenCount":9,"candidatesTokenCount":16,"totalTokenCount":25},"modelVersion":"gemini-3.5-flash-lite"}`
)

var streamProviders = []Provider{ProviderGoogleAI, ProviderVertexAI}

func streamHandler(chunks ...string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !isStreamRequest(r) {
			http.NotFound(w, r)
			return
		}
		writeSSE(w, chunks...)
	}
}

func drainStream(t *testing.T, c *stubbedClient) {
	t.Helper()
	stream := c.client.Models().GenerateContentStream(context.Background(), "gemini-3.5-flash-lite", genai.Text("Write a long story about a fox."), nil)
	for _, err := range stream {
		require.NoError(t, err)
	}
}

func TestStreamRecordsFinishReasonOfFinalChunk(t *testing.T) {
	cases := []struct {
		name   string
		chunks []string
		want   string
	}{
		{"max tokens", []string{geminiTextChunk, geminiMaxTokensChunk}, "TOKEN_LIMIT"},
		{"safety stop", []string{geminiTextChunk, geminiSafetyChunk}, "ERROR"},
		{"continuation", []string{geminiTextChunk, geminiContinuationChunk}, "TOKEN_LIMIT"},
		{"usage-only trailer after max tokens", []string{geminiTextChunk, geminiMaxTokensChunk, geminiUsageOnlyChunk}, "TOKEN_LIMIT"},
	}
	for _, provider := range streamProviders {
		for _, tc := range cases {
			t.Run(provider.String()+" "+tc.name, func(t *testing.T) {
				c := newStubbedClient(t, provider, streamHandler(tc.chunks...))

				drainStream(t, c)

				record := singleRecord(t, c)
				assert.Equal(t, tc.want, record["stopReason"])
				assert.Equal(t, true, record["isStreamed"])
			})
		}
	}
}

func TestStreamStoppedByConsumerKeepsFinishReasonSeen(t *testing.T) {
	cases := []struct {
		name       string
		stopAfter  int
		wantReason string
	}{
		{"stopped on the max tokens chunk", 2, "TOKEN_LIMIT"},
		{"stopped before any finish reason", 1, "CANCELLED"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := newStubbedClient(t, ProviderGoogleAI, streamHandler(geminiTextChunk, geminiMaxTokensChunk, geminiUsageOnlyChunk))

			received := 0
			for _, err := range c.client.Models().GenerateContentStream(context.Background(), "gemini-3.5-flash-lite", genai.Text("Write a story."), nil) {
				require.NoError(t, err)
				received++
				if received == tc.stopAfter {
					break
				}
			}

			assert.Equal(t, tc.wantReason, singleRecord(t, c)["stopReason"])
		})
	}
}

func TestStreamErrorRecordsSameStopReasonAsNonStreamedError(t *testing.T) {
	c := newStubbedClient(t, ProviderGoogleAI, func(w http.ResponseWriter, r *http.Request) {
		if isStreamRequest(r) {
			writeSSE(w, geminiTextChunk)
			_, _ = w.Write([]byte(`{"error":{"code":500,"message":"backend failure","status":"INTERNAL"}}` + "\r\n"))
			return
		}
		writeJSON(w, http.StatusInternalServerError, `{"error":{"code":500,"message":"backend failure","status":"INTERNAL"}}`)
	})

	var streamErr error
	for _, err := range c.client.Models().GenerateContentStream(context.Background(), "gemini-3.5-flash-lite", genai.Text("hi"), nil) {
		if err != nil {
			streamErr = err
		}
	}
	require.Error(t, streamErr)
	_, err := c.client.Models().GenerateContent(context.Background(), "gemini-3.5-flash-lite", genai.Text("hi"), nil)
	require.Error(t, err)

	records := c.records(t)
	require.Len(t, records, 2)
	assert.Equal(t, records[0]["stopReason"], records[1]["stopReason"])
	assert.Equal(t, "ERROR", records[0]["stopReason"])
}

func TestNonStreamedContinuationIsTokenLimit(t *testing.T) {
	c := newStubbedClient(t, ProviderGoogleAI, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, geminiContinuationChunk)
	})

	_, err := c.client.Models().GenerateContent(context.Background(), "gemini-3.5-flash-lite", genai.Text("Write a story."), nil)
	require.NoError(t, err)

	assert.Equal(t, "TOKEN_LIMIT", singleRecord(t, c)["stopReason"])
}

func TestStreamAbandonedBeforeFinishIsCancelledAndCapturedAsTruncated(t *testing.T) {
	t.Setenv(core.EnvCapturePrompts, "true")
	for _, provider := range streamProviders {
		t.Run(provider.String(), func(t *testing.T) {
			c := newStubbedClient(t, provider, streamHandler(geminiTextChunk, geminiMaxTokensChunk, geminiUsageOnlyChunk))

			for _, err := range c.client.Models().GenerateContentStream(context.Background(), "gemini-3.5-flash-lite", genai.Text("Write a story."), nil) {
				require.NoError(t, err)
				break
			}

			record := singleRecord(t, c)
			assert.Equal(t, "CANCELLED", record["stopReason"])
			assert.Equal(t, true, record["promptsTruncated"])
			assert.Equal(t, 13.0, record["totalTokenCount"])
		})
	}
}

func TestStreamAbandonedAfterItsFinishReasonKeepsThatStopReasonAndIsComplete(t *testing.T) {
	t.Setenv(core.EnvCapturePrompts, "true")
	c := newStubbedClient(t, ProviderGoogleAI, streamHandler(geminiTextChunk, geminiMaxTokensChunk, geminiUsageOnlyChunk))

	received := 0
	for _, err := range c.client.Models().GenerateContentStream(context.Background(), "gemini-3.5-flash-lite", genai.Text("Write a story."), nil) {
		require.NoError(t, err)
		received++
		if received == 2 {
			break
		}
	}

	record := singleRecord(t, c)
	assert.Equal(t, "TOKEN_LIMIT", record["stopReason"])
	assert.Nil(t, record["promptsTruncated"])
}
