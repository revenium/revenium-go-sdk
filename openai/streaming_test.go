package openai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	openaisdk "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/azure"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/shared/constant"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/revenium/revenium-go-sdk/core/metering"
	"github.com/revenium/revenium-go-sdk/core/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestOpenAI(t *testing.T, openaiURL, meteringURL string) *ReveniumOpenAI {
	t.Helper()
	cfg := &Config{
		OpenAIAPIKey: "sk-test",
		BaseURL:      openaiURL,
		Revenium: &core.ReveniumConfig{
			APIKey:  "hak_test_key_123",
			BaseURL: meteringURL,
		},
	}
	r, err := NewReveniumOpenAI(cfg)
	require.NoError(t, err)
	return r
}

func writeSSE(t *testing.T, w http.ResponseWriter, line string) {
	t.Helper()
	if _, err := w.Write([]byte(line)); err != nil {
		t.Errorf("writeSSE: %v", err)
		return
	}
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}

func sseChatHandler(t *testing.T) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		writeSSE(t, w, `data: {"id":"chatcmpl-1","object":"chat.completion.chunk","created":1700000000,"model":"gpt-4o-mini","choices":[{"index":0,"delta":{"role":"assistant","content":"hello"},"finish_reason":null}]}`+"\n\n")
		writeSSE(t, w, `data: {"id":"chatcmpl-1","object":"chat.completion.chunk","created":1700000000,"model":"gpt-4o-mini","choices":[{"index":0,"delta":{"content":" world"},"finish_reason":null}]}`+"\n\n")
		writeSSE(t, w, `data: {"id":"chatcmpl-1","object":"chat.completion.chunk","created":1700000000,"model":"gpt-4o-mini","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":11,"completion_tokens":2,"total_tokens":13}}`+"\n\n")
		writeSSE(t, w, "data: [DONE]\n\n")
	})
}

func TestStreaming_FullCycle(t *testing.T) {
	openaiServer := httptest.NewServer(sseChatHandler(t))
	defer openaiServer.Close()

	mock := testutil.NewMockMeteringServer()
	defer mock.Close()

	r := newTestOpenAI(t, openaiServer.URL, mock.URL())
	defer r.Close()

	ctx := core.WithUsageMetadata(context.Background(), map[string]interface{}{
		"traceId": "stream-trace-1",
	})

	stream, err := r.Chat().Completions().NewStreaming(ctx, openaisdk.ChatCompletionNewParams{
		Model: "gpt-4o-mini",
		Messages: []openaisdk.ChatCompletionMessageParamUnion{
			{OfUser: &openaisdk.ChatCompletionUserMessageParam{
				Role:    constant.ValueOf[constant.User](),
				Content: openaisdk.ChatCompletionUserMessageParamContentUnion{OfString: openaisdk.String("Say hi")},
			}},
		},
	})
	require.NoError(t, err)

	chunks := 0
	var content string
	for stream.Next() {
		chunks++
		c := stream.Current()
		if len(c.Choices) > 0 {
			content += c.Choices[0].Delta.Content
		}
	}
	require.NoError(t, stream.Err())
	require.NoError(t, stream.Close())
	r.Flush()

	assert.GreaterOrEqual(t, chunks, 2)
	assert.Equal(t, "hello world", content)

	require.True(t, mock.WaitForPayloads(1, 2*time.Second))
	payloads := mock.GetPayloads()
	require.Len(t, payloads, 1)
	p := payloads[0]
	assert.Equal(t, "CHAT", p["operationType"])
	assert.Equal(t, "gpt-4o-mini", p["model"])
	assert.Equal(t, true, p["isStreamed"])
	assert.Equal(t, float64(11), p["inputTokenCount"])
	assert.Equal(t, float64(2), p["outputTokenCount"])
	assert.Equal(t, float64(13), p["totalTokenCount"])
	assert.Equal(t, "END", p["stopReason"])
	assert.Equal(t, "stream-trace-1", p["traceId"])
}

type usageOnRequestProvider struct {
	mu           sync.Mutex
	includeUsage []bool
}

func (p *usageOnRequestProvider) handler(t *testing.T) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			StreamOptions struct {
				IncludeUsage bool `json:"include_usage"`
			} `json:"stream_options"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		p.mu.Lock()
		p.includeUsage = append(p.includeUsage, body.StreamOptions.IncludeUsage)
		p.mu.Unlock()

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		writeSSE(t, w, `data: {"id":"chatcmpl-2","object":"chat.completion.chunk","created":1700000000,"model":"gpt-4.1-nano","choices":[{"index":0,"delta":{"role":"assistant","content":"Hi"},"finish_reason":null}]}`+"\n\n")
		writeSSE(t, w, `data: {"id":"chatcmpl-2","object":"chat.completion.chunk","created":1700000000,"model":"gpt-4.1-nano","choices":[{"index":0,"delta":{"content":" there"},"finish_reason":null}]}`+"\n\n")
		writeSSE(t, w, `data: {"id":"chatcmpl-2","object":"chat.completion.chunk","created":1700000000,"model":"gpt-4.1-nano","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`+"\n\n")
		if body.StreamOptions.IncludeUsage {
			writeSSE(t, w, `data: {"id":"chatcmpl-2","object":"chat.completion.chunk","created":1700000000,"model":"gpt-4.1-nano","choices":[],"usage":{"prompt_tokens":13,"completion_tokens":3,"total_tokens":16}}`+"\n\n")
		}
		writeSSE(t, w, "data: [DONE]\n\n")
	})
}

func (p *usageOnRequestProvider) requestedUsage() []bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]bool(nil), p.includeUsage...)
}

func newTestAzureOpenAI(t *testing.T, providerServer *httptest.Server, meteringURL string) *ReveniumOpenAI {
	t.Helper()
	mc, err := metering.NewMeteringClient(metering.MeteringClientConfig{APIKey: "hak_test_key_123", BaseURL: meteringURL})
	require.NoError(t, err)
	client := openaisdk.NewClient(
		azure.WithEndpoint(providerServer.URL, "2024-10-21"),
		azure.WithAPIKey("azure-test-key"),
		option.WithHTTPClient(providerServer.Client()),
	)
	return &ReveniumOpenAI{client: client, config: &Config{}, provider: ProviderAzure, metering: mc}
}

func newTestOpenAIFromServer(t *testing.T, providerServer *httptest.Server, meteringURL string) *ReveniumOpenAI {
	return newTestOpenAI(t, providerServer.URL, meteringURL)
}

func readmeStreamingParams() openaisdk.ChatCompletionNewParams {
	return openaisdk.ChatCompletionNewParams{
		Model:    "gpt-4.1-nano",
		Messages: []openaisdk.ChatCompletionMessageParamUnion{openaisdk.UserMessage("Say hi in two words.")},
	}
}

type streamedChunks struct {
	total             int
	withoutChoices    int
	contentDelivered  string
	usageChunkVisible bool
}

func drainStream(t *testing.T, stream *StreamingWrapper) streamedChunks {
	t.Helper()
	var seen streamedChunks
	for stream.Next() {
		chunk := stream.Current()
		seen.total++
		if len(chunk.Choices) == 0 {
			seen.withoutChoices++
			seen.usageChunkVisible = seen.usageChunkVisible || chunk.JSON.Usage.Valid()
			continue
		}
		seen.contentDelivered += chunk.Choices[0].Delta.Content
	}
	require.NoError(t, stream.Err())
	return seen
}

func TestStreaming_ReadmeShapedCallIsMeteredWithProviderUsage(t *testing.T) {
	cases := []struct {
		name     string
		provider string
		server   func(http.Handler) *httptest.Server
		newSDK   func(t *testing.T, providerServer *httptest.Server, meteringURL string) *ReveniumOpenAI
	}{
		{name: "openai", provider: "OPENAI", server: httptest.NewServer, newSDK: newTestOpenAIFromServer},
		{name: "azure", provider: "AZURE", server: httptest.NewTLSServer, newSDK: newTestAzureOpenAI},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			provider := &usageOnRequestProvider{}
			providerServer := tc.server(provider.handler(t))
			defer providerServer.Close()

			mock := testutil.NewMockMeteringServer()
			defer mock.Close()

			r := tc.newSDK(t, providerServer, mock.URL())
			defer r.Close()

			params := readmeStreamingParams()
			stream, err := r.Chat().Completions().NewStreaming(context.Background(), params)
			require.NoError(t, err)

			seen := drainStream(t, stream)
			require.NoError(t, stream.Close())
			r.Flush()

			assert.Equal(t, []bool{true}, provider.requestedUsage(), "the SDK must ask the provider to report usage")
			assert.False(t, params.StreamOptions.IncludeUsage.Valid(), "the caller's params must not be modified")
			assert.Equal(t, "Hi there", seen.contentDelivered)
			assert.Zero(t, seen.withoutChoices, "a usage chunk the caller did not ask for must stay hidden")

			require.True(t, mock.WaitForPayloads(1, 2*time.Second))
			payloads := mock.GetPayloads()
			require.Len(t, payloads, 1)
			p := payloads[0]
			assert.Equal(t, tc.provider, p["provider"])
			assert.Equal(t, true, p["isStreamed"])
			assert.Equal(t, float64(13), p["inputTokenCount"])
			assert.Equal(t, float64(3), p["outputTokenCount"])
			assert.Equal(t, float64(16), p["totalTokenCount"])
			assert.Equal(t, "END", p["stopReason"])
		})
	}
}

func TestStreaming_CallerRequestedUsageIsMeteredOnceAndStillDelivered(t *testing.T) {
	provider := &usageOnRequestProvider{}
	providerServer := httptest.NewServer(provider.handler(t))
	defer providerServer.Close()

	mock := testutil.NewMockMeteringServer()
	defer mock.Close()

	r := newTestOpenAI(t, providerServer.URL, mock.URL())
	defer r.Close()

	params := readmeStreamingParams()
	params.StreamOptions = openaisdk.ChatCompletionStreamOptionsParam{IncludeUsage: openaisdk.Bool(true)}
	stream, err := r.Chat().Completions().NewStreaming(context.Background(), params)
	require.NoError(t, err)

	seen := drainStream(t, stream)
	require.NoError(t, stream.Close())
	r.Flush()

	assert.Equal(t, []bool{true}, provider.requestedUsage())
	assert.True(t, seen.usageChunkVisible, "a caller who asked for usage must still receive the usage chunk")

	require.True(t, mock.WaitForPayloads(1, 2*time.Second))
	time.Sleep(100 * time.Millisecond)
	payloads := mock.GetPayloads()
	require.Len(t, payloads, 1)
	assert.Equal(t, float64(13), payloads[0]["inputTokenCount"])
	assert.Equal(t, float64(3), payloads[0]["outputTokenCount"])
	assert.Equal(t, float64(16), payloads[0]["totalTokenCount"])
}

func TestStreaming_UsageIsCapturedWhenCallerOnlyDrainsWithNext(t *testing.T) {
	provider := &usageOnRequestProvider{}
	providerServer := httptest.NewServer(provider.handler(t))
	defer providerServer.Close()

	mock := testutil.NewMockMeteringServer()
	defer mock.Close()

	r := newTestOpenAI(t, providerServer.URL, mock.URL())
	defer r.Close()

	stream, err := r.Chat().Completions().NewStreaming(context.Background(), readmeStreamingParams())
	require.NoError(t, err)
	for stream.Next() {
	}
	require.NoError(t, stream.Close())
	r.Flush()

	require.True(t, mock.WaitForPayloads(1, 2*time.Second))
	assert.Equal(t, float64(13), mock.GetPayloads()[0]["inputTokenCount"])
}
