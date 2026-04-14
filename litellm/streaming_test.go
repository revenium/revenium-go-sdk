package litellm

import (
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type nopCloser struct{ io.Reader }

func (nopCloser) Close() error { return nil }

func newStreamFromString(body string) *StreamingResponse {
	return newStreamingResponse(nopCloser{strings.NewReader(body)}, nil, "openai/gpt-4", nil, false)
}

func TestStreaming_ParsesBasicChunks(t *testing.T) {
	body := "data: {\"id\":\"resp-1\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"Hi\"}}]}\n\n" +
		"data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"!\"},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":5,\"completion_tokens\":2,\"total_tokens\":7}}\n\n" +
		"data: [DONE]\n\n"
	s := newStreamFromString(body)
	chunks := 0
	for s.Next() {
		chunks++
	}
	require.NoError(t, s.Err())
	assert.Equal(t, 2, chunks)
	assert.Equal(t, int64(5), s.inputTokens)
	assert.Equal(t, int64(7), s.totalTokens)
	assert.Equal(t, "stop", s.finishReason)
}

func TestStreaming_LargeChunkOver64KB(t *testing.T) {
	bigContent := strings.Repeat("a", 128*1024)
	chunk := StreamChunk{
		Choices: []StreamChoice{{Index: 0, Delta: StreamDelta{Content: bigContent}}},
	}
	raw, err := json.Marshal(chunk)
	require.NoError(t, err)

	body := "data: " + string(raw) + "\n\ndata: [DONE]\n\n"
	s := newStreamFromString(body)
	got := 0
	for s.Next() {
		got++
		assert.Equal(t, bigContent, s.Current().Choices[0].Delta.Content)
	}
	require.NoError(t, s.Err())
	assert.Equal(t, 1, got)
}

func TestStreaming_XGroqUsageFallback(t *testing.T) {
	body := "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"x\"}}],\"x_groq\":{\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":20,\"total_tokens\":30}}}\n\n" +
		"data: [DONE]\n\n"
	s := newStreamFromString(body)
	for s.Next() {
	}
	require.NoError(t, s.Err())
	assert.Equal(t, int64(10), s.inputTokens)
	assert.Equal(t, int64(20), s.outputTokens)
	assert.Equal(t, int64(30), s.totalTokens)
}

func TestStreaming_ReconstructResponseDisabledWhenCaptureOff(t *testing.T) {
	body := "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hello\"}}]}\n\ndata: [DONE]\n\n"
	s := newStreamFromString(body)
	for s.Next() {
	}
	assert.Nil(t, s.ReconstructResponse())
}

func TestStreaming_ReconstructResponseWithCapture(t *testing.T) {
	os.Setenv("REVENIUM_CAPTURE_PROMPTS", "true")
	defer os.Unsetenv("REVENIUM_CAPTURE_PROMPTS")

	body := "data: {\"id\":\"abc\",\"created\":123,\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hel\"}}]}\n\n" +
		"data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"lo\"},\"finish_reason\":\"stop\"}]}\n\n" +
		"data: [DONE]\n\n"
	s := newStreamFromString(body)
	for s.Next() {
	}
	resp := s.ReconstructResponse()
	require.NotNil(t, resp)
	assert.Equal(t, "abc", resp.ID)
	assert.Equal(t, int64(123), resp.Created)
	assert.Equal(t, "hello", resp.Choices[0].Message.Content)
	assert.Equal(t, "stop", resp.Choices[0].FinishReason)
}

func TestStreaming_ToolCallAccumulationAcrossChunks(t *testing.T) {
	os.Setenv("REVENIUM_CAPTURE_PROMPTS", "true")
	defer os.Unsetenv("REVENIUM_CAPTURE_PROMPTS")

	body := "data: {\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"type\":\"function\",\"function\":{\"name\":\"lookup\",\"arguments\":\"{\\\"q\\\":\"}}]}}]}\n\n" +
		"data: {\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\"\\\"weather\\\"}\"}}]}}]}\n\n" +
		"data: [DONE]\n\n"
	s := newStreamFromString(body)
	for s.Next() {
	}
	resp := s.ReconstructResponse()
	require.NotNil(t, resp)
	require.Len(t, resp.Choices[0].Message.ToolCalls, 1)
	tc := resp.Choices[0].Message.ToolCalls[0]
	assert.Equal(t, "call_1", tc.ID)
	assert.Equal(t, "function", tc.Type)
	assert.Equal(t, "lookup", tc.Function.Name)
	assert.Equal(t, `{"q":"weather"}`, tc.Function.Arguments)
}
