package openai

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"

	openaisdk "github.com/openai/openai-go/v3"

	"github.com/revenium/revenium-go-sdk/core/enforcement"
	"github.com/revenium/revenium-go-sdk/core/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const textCompletionAnswer = `{"id":"cmpl-1","object":"text_completion","created":1700000000,"model":"gpt-3.5-turbo-instruct",` +
	`"choices":[{"index":0,"text":"hi","finish_reason":"length","logprobs":null}],` +
	`"usage":{"prompt_tokens":5,"completion_tokens":3,"total_tokens":8}}`

type textCompletionsStub struct {
	server        *httptest.Server
	hits          atomic.Int32
	mu            sync.Mutex
	paths         []string
	includedUsage []bool
}

func newTextCompletionsStub(t *testing.T) *textCompletionsStub {
	t.Helper()
	stub := &textCompletionsStub{}
	stub.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		stub.hits.Add(1)
		var body struct {
			Stream        bool `json:"stream"`
			StreamOptions struct {
				IncludeUsage bool `json:"include_usage"`
			} `json:"stream_options"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		stub.mu.Lock()
		stub.paths = append(stub.paths, r.URL.Path)
		stub.includedUsage = append(stub.includedUsage, body.StreamOptions.IncludeUsage)
		stub.mu.Unlock()

		if !body.Stream {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(textCompletionAnswer))
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		writeSSE(t, w, `data: {"id":"cmpl-1","object":"text_completion","created":1700000000,"model":"gpt-3.5-turbo-instruct","choices":[{"index":0,"text":"h","finish_reason":null}]}`+"\n\n")
		writeSSE(t, w, `data: {"id":"cmpl-1","object":"text_completion","created":1700000000,"model":"gpt-3.5-turbo-instruct","choices":[{"index":0,"text":"i","finish_reason":"stop"}]}`+"\n\n")
		writeSSE(t, w, `data: {"id":"cmpl-1","object":"text_completion","created":1700000000,"model":"gpt-3.5-turbo-instruct","choices":[],"usage":{"prompt_tokens":6,"completion_tokens":2,"total_tokens":8}}`+"\n\n")
		writeSSE(t, w, "data: [DONE]\n\n")
	}))
	t.Cleanup(stub.server.Close)
	return stub
}

func textCompletionParams() openaisdk.CompletionNewParams {
	return openaisdk.CompletionNewParams{
		Model:     openaisdk.CompletionNewParamsModelGPT3_5TurboInstruct,
		Prompt:    openaisdk.CompletionNewParamsPromptUnion{OfString: openaisdk.String("say hi")},
		MaxTokens: openaisdk.Int(3),
	}
}

func TestCompletions_RecordsOneUsageRecordPerCall(t *testing.T) {
	provider := newTextCompletionsStub(t)
	mock := testutil.NewMockMeteringServer()
	t.Cleanup(mock.Close)
	r := newTestOpenAI(t, provider.server.URL, mock.URL())
	t.Cleanup(func() { _ = r.Close() })

	for range 2 {
		_, err := r.Completions().New(asSubscriber("alice@example.com"), textCompletionParams())
		require.NoError(t, err)
	}

	assert.Equal(t, []string{"/completions", "/completions"}, provider.paths)
	r.Flush()
	records := mock.GetPayloads()
	require.Len(t, records, 2)
	assert.Equal(t, "gpt-3.5-turbo-instruct", records[0]["model"])
	assert.Equal(t, float64(5), records[0]["inputTokenCount"])
	assert.Equal(t, float64(3), records[0]["outputTokenCount"])
	assert.Equal(t, "TOKEN_LIMIT", records[0]["stopReason"])
	assert.Equal(t, map[string]interface{}{"email": "alice@example.com"}, records[0]["subscriber"])
}

func TestCompletions_StreamRecordsOnceAndHidesTheUsageChunkItAskedFor(t *testing.T) {
	provider := newTextCompletionsStub(t)
	mock := testutil.NewMockMeteringServer()
	t.Cleanup(mock.Close)
	r := newTestOpenAI(t, provider.server.URL, mock.URL())
	t.Cleanup(func() { _ = r.Close() })

	stream, err := r.Completions().NewStreaming(asSubscriber("alice@example.com"), textCompletionParams())
	require.NoError(t, err)
	chunks := 0
	for stream.Next() {
		assert.NotEmpty(t, stream.Current().Choices)
		chunks++
	}
	require.NoError(t, stream.Err())
	require.NoError(t, stream.Close())
	require.NoError(t, stream.Close())

	assert.Equal(t, 2, chunks)
	assert.Equal(t, []bool{true}, provider.includedUsage)
	r.Flush()
	records := mock.GetPayloads()
	require.Len(t, records, 1)
	assert.Equal(t, true, records[0]["isStreamed"])
	assert.Equal(t, float64(6), records[0]["inputTokenCount"])
	assert.Equal(t, float64(2), records[0]["outputTokenCount"])
}

func TestCompletions_PassTheCostControlGate(t *testing.T) {
	enforcement.Stop()
	t.Cleanup(enforcement.Stop)
	provider := newTextCompletionsStub(t)
	rules := testutil.NewMockRulesServer()
	t.Cleanup(rules.Close)
	metering := testutil.NewMockMeteringServer()
	t.Cleanup(metering.Close)
	rules.SetPayload(enforcementTestTeam, breachedRule(`"filters": [], "metricType": "TOTAL_COST", "groupBy": "SUBSCRIBER",
		"groupBreakdown": [{"groupValue": "alice@example.com", "currentValue": 61, "breached": true}]`))

	completions := newEnforcedOpenAI(t, provider.server.URL, rules.URL(), metering.URL(), enforcementTestTeam).Completions()
	var ece *enforcement.ErrCostLimitExceeded

	_, err := completions.New(asSubscriber("alice@example.com"), textCompletionParams())
	require.True(t, errors.As(err, &ece), "alice's completion should be blocked, got %v", err)
	_, err = completions.NewStreaming(asSubscriber("alice@example.com"), textCompletionParams())
	require.True(t, errors.As(err, &ece), "alice's streamed completion should be blocked, got %v", err)
	assert.Equal(t, int32(0), provider.hits.Load(), "a blocked completion never reaches OpenAI")

	_, err = completions.New(asSubscriber("bob@example.com"), textCompletionParams())
	require.NoError(t, err)
	assert.Equal(t, int32(1), provider.hits.Load())
}
