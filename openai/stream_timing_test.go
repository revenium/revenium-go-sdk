package openai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	openaisdk "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"
	"github.com/revenium/revenium-go-sdk/core/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// lateClose is more than one second past the slack, so a responseTime taken at Close()
// always lands in a later second than the stream end, the wire precision of the field.
const (
	headerDelay   = 300 * time.Millisecond
	firstTokenLag = 200 * time.Millisecond
	finishLag     = 150 * time.Millisecond
	lateClose     = 1300 * time.Millisecond
	timingSlack   = 250 * time.Millisecond

	wantFirstToken = headerDelay + firstTokenLag
	wantDuration   = wantFirstToken + finishLag
)

type slowStreamChunks struct {
	opening string
	output  string
	finish  string
	trailer string
}

var slowChatChunks = slowStreamChunks{
	opening: `{"id":"c1","object":"chat.completion.chunk","created":1700000000,"model":"gpt-4.1-nano","choices":[{"index":0,"delta":{"role":"assistant","content":""},"finish_reason":null}]}`,
	output:  `{"id":"c1","object":"chat.completion.chunk","created":1700000000,"model":"gpt-4.1-nano","choices":[{"index":0,"delta":{"content":"hi"},"finish_reason":null}]}`,
	finish:  `{"id":"c1","object":"chat.completion.chunk","created":1700000000,"model":"gpt-4.1-nano","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
	trailer: `{"id":"c1","object":"chat.completion.chunk","created":1700000000,"model":"gpt-4.1-nano","choices":[],"usage":{"prompt_tokens":7,"completion_tokens":1,"total_tokens":8}}`,
}

var slowTextChunks = slowStreamChunks{
	opening: `{"id":"t1","object":"text_completion","created":1700000000,"model":"gpt-3.5-turbo-instruct","choices":[{"index":0,"text":"","finish_reason":null}]}`,
	output:  `{"id":"t1","object":"text_completion","created":1700000000,"model":"gpt-3.5-turbo-instruct","choices":[{"index":0,"text":"hi","finish_reason":null}]}`,
	finish:  `{"id":"t1","object":"text_completion","created":1700000000,"model":"gpt-3.5-turbo-instruct","choices":[{"index":0,"text":"","finish_reason":"stop"}]}`,
	trailer: `{"id":"t1","object":"text_completion","created":1700000000,"model":"gpt-3.5-turbo-instruct","choices":[],"usage":{"prompt_tokens":7,"completion_tokens":1,"total_tokens":8}}`,
}

var slowResponsesEvents = slowStreamChunks{
	opening: `{"type":"response.in_progress","sequence_number":0,"response":{"id":"resp_1","object":"response","created_at":1700000000,"model":"gpt-4.1-nano","status":"in_progress","output":[]}}`,
	output:  responsesTextDelta,
	finish:  responsesCompleted,
}

var twoChoiceChatChunks = slowStreamChunks{
	opening: `{"id":"c2","object":"chat.completion.chunk","created":1700000000,"model":"gpt-4.1-nano","choices":[{"index":0,"delta":{"role":"assistant","content":""},"finish_reason":null},{"index":1,"delta":{"role":"assistant","content":""},"finish_reason":null}]}`,
	output:  `{"id":"c2","object":"chat.completion.chunk","created":1700000000,"model":"gpt-4.1-nano","choices":[{"index":0,"delta":{"content":"a"},"finish_reason":"stop"},{"index":1,"delta":{"content":"b"},"finish_reason":null}]}`,
	finish:  `{"id":"c2","object":"chat.completion.chunk","created":1700000000,"model":"gpt-4.1-nano","choices":[{"index":1,"delta":{},"finish_reason":"stop"}]}`,
	trailer: `{"id":"c2","object":"chat.completion.chunk","created":1700000000,"model":"gpt-4.1-nano","choices":[],"usage":{"prompt_tokens":7,"completion_tokens":2,"total_tokens":9}}`,
}

var twoChoiceTextChunks = slowStreamChunks{
	opening: `{"id":"t2","object":"text_completion","created":1700000000,"model":"gpt-3.5-turbo-instruct","choices":[{"index":0,"text":"","finish_reason":null},{"index":1,"text":"","finish_reason":null}]}`,
	output:  `{"id":"t2","object":"text_completion","created":1700000000,"model":"gpt-3.5-turbo-instruct","choices":[{"index":0,"text":"a","finish_reason":"stop"},{"index":1,"text":"b","finish_reason":null}]}`,
	finish:  `{"id":"t2","object":"text_completion","created":1700000000,"model":"gpt-3.5-turbo-instruct","choices":[{"index":1,"text":"","finish_reason":"stop"}]}`,
	trailer: slowTextChunks.trailer,
}

func withoutFinish(chunks slowStreamChunks) slowStreamChunks {
	chunks.finish = ""
	return chunks
}

func slowStreamServer(t *testing.T, chunks slowStreamChunks) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(headerDelay)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		writeSSE(t, w, "data: "+chunks.opening+"\n\n")
		time.Sleep(firstTokenLag)
		writeSSE(t, w, "data: "+chunks.output+"\n\n")
		time.Sleep(finishLag)
		if chunks.finish != "" {
			writeSSE(t, w, "data: "+chunks.finish+"\n\n")
		}
		if chunks.trailer != "" {
			writeSSE(t, w, "data: "+chunks.trailer+"\n\n")
		}
		writeSSE(t, w, "data: [DONE]\n\n")
	}))
	t.Cleanup(server.Close)
	return server
}

type nextReader interface {
	Next() bool
	Close() error
}

func readThenCloseLate(t *testing.T, stream nextReader, stopAtFinish func() bool) {
	t.Helper()
	for stream.Next() {
		if stopAtFinish() {
			break
		}
	}
	time.Sleep(lateClose)
	require.NoError(t, stream.Close())
}

func singleTimingRecord(t *testing.T, r *ReveniumOpenAI, mock *testutil.MockMeteringServer) map[string]interface{} {
	t.Helper()
	r.Flush()
	require.True(t, mock.WaitForPayloads(1, 2*time.Second))
	records := mock.GetPayloads()
	require.Len(t, records, 1)
	return records[0]
}

func assertMeasuredLikeTheWallClock(t *testing.T, record map[string]interface{}, callStart time.Time) {
	t.Helper()
	firstToken := time.Duration(record["timeToFirstToken"].(float64)) * time.Millisecond
	duration := time.Duration(record["requestDuration"].(float64)) * time.Millisecond

	assert.GreaterOrEqual(t, firstToken, wantFirstToken, "first token is timed from before the request is sent, not after the headers or the role-only chunk")
	assert.Less(t, firstToken, wantFirstToken+timingSlack)
	assert.GreaterOrEqual(t, duration, wantDuration)
	assert.Less(t, duration, wantDuration+timingSlack, "a late Close() must not stretch the duration")

	requestTime, err := time.Parse(time.RFC3339, record["requestTime"].(string))
	require.NoError(t, err)
	assert.False(t, requestTime.After(callStart), "requestTime is taken before the request is sent")
	responseTime, err := time.Parse(time.RFC3339, record["responseTime"].(string))
	require.NoError(t, err)
	latestStreamEnd := callStart.Add(wantDuration + timingSlack).Truncate(time.Second)
	assert.False(t, responseTime.After(latestStreamEnd), "responseTime is the end of the stream, not the Close() call")
}

func newTimingClient(t *testing.T, server *httptest.Server) (*ReveniumOpenAI, *testutil.MockMeteringServer) {
	t.Helper()
	mock := testutil.NewMockMeteringServer()
	t.Cleanup(mock.Close)
	r := newTestOpenAI(t, server.URL, mock.URL())
	t.Cleanup(func() { _ = r.Close() })
	return r, mock
}

func TestChatStreaming_TimesTheCallNotTheClose(t *testing.T) {
	r, mock := newTimingClient(t, slowStreamServer(t, slowChatChunks))

	callStart := time.Now()
	stream, err := r.Chat().Completions().NewStreaming(context.Background(), readmeStreamingParams())
	require.NoError(t, err)
	readThenCloseLate(t, stream, func() bool { return false })

	assertMeasuredLikeTheWallClock(t, singleTimingRecord(t, r, mock), callStart)
}

func TestChatStreaming_EndsAtTheFinishReasonWhenTheCallerStopsReadingThere(t *testing.T) {
	r, mock := newTimingClient(t, slowStreamServer(t, slowChatChunks))

	callStart := time.Now()
	stream, err := r.Chat().Completions().NewStreaming(context.Background(), readmeStreamingParams())
	require.NoError(t, err)
	readThenCloseLate(t, stream, func() bool { return chunkFinished(stream.Current()) })

	assertMeasuredLikeTheWallClock(t, singleTimingRecord(t, r, mock), callStart)
}

func TestTextCompletionStreaming_TimesTheCallNotTheClose(t *testing.T) {
	r, mock := newTimingClient(t, slowStreamServer(t, slowTextChunks))

	callStart := time.Now()
	stream, err := r.Completions().NewStreaming(context.Background(), textCompletionParams())
	require.NoError(t, err)
	readThenCloseLate(t, stream, func() bool { return false })

	assertMeasuredLikeTheWallClock(t, singleTimingRecord(t, r, mock), callStart)
}

func TestResponsesStreaming_TimesTheCallNotTheClose(t *testing.T) {
	r, mock := newTimingClient(t, slowStreamServer(t, slowResponsesEvents))

	callStart := time.Now()
	stream, err := r.Responses().CreateStreaming(context.Background(), responses.ResponseNewParams{Model: "gpt-4.1-nano"})
	require.NoError(t, err)
	readThenCloseLate(t, stream, func() bool { return false })

	assertMeasuredLikeTheWallClock(t, singleTimingRecord(t, r, mock), callStart)
}

func TestResponsesStreaming_EndsAtTheTerminalEventWhenTheCallerStopsReadingThere(t *testing.T) {
	r, mock := newTimingClient(t, slowStreamServer(t, slowResponsesEvents))

	callStart := time.Now()
	stream, err := r.Responses().CreateStreaming(context.Background(), responses.ResponseNewParams{Model: "gpt-4.1-nano"})
	require.NoError(t, err)
	readThenCloseLate(t, stream, func() bool { return isFinalResponseEvent(stream.Current().Type) })

	assertMeasuredLikeTheWallClock(t, singleTimingRecord(t, r, mock), callStart)
}

func TestIsOutputDeltaEvent_CountsOnlyDeltasThatCarryOutput(t *testing.T) {
	cases := map[string]struct {
		event responses.ResponseStreamEventUnion
		want  bool
	}{
		"text delta":        {responses.ResponseStreamEventUnion{Type: "response.output_text.delta", Delta: "a"}, true},
		"empty text delta":  {responses.ResponseStreamEventUnion{Type: "response.output_text.delta"}, false},
		"refusal delta":     {responses.ResponseStreamEventUnion{Type: "response.refusal.delta", Delta: "no"}, true},
		"tool arguments":    {responses.ResponseStreamEventUnion{Type: "response.function_call_arguments.delta", Delta: "{"}, true},
		"created":           {responses.ResponseStreamEventUnion{Type: "response.created"}, false},
		"reasoning summary": {responses.ResponseStreamEventUnion{Type: "response.reasoning_summary_text.delta", Delta: "x"}, false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tc.want, isOutputDeltaEvent(tc.event))
		})
	}
}

func TestChatChunkHasOutput_CountsAnythingTheModelProduced(t *testing.T) {
	cases := map[string]struct {
		chunk string
		want  bool
	}{
		"role only":     {`{"choices":[{"index":0,"delta":{"role":"assistant","content":""}}]}`, false},
		"usage only":    {`{"choices":[],"usage":{"prompt_tokens":1}}`, false},
		"text":          {`{"choices":[{"index":0,"delta":{"content":"a"}}]}`, true},
		"refusal":       {`{"choices":[{"index":0,"delta":{"refusal":"no"}}]}`, true},
		"tool call":     {`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"name":"f"}}]}}]}`, true},
		"function call": {`{"choices":[{"index":0,"delta":{"function_call":{"arguments":"{"}}}]}`, true},
		"second choice": {`{"choices":[{"index":0,"delta":{}},{"index":1,"delta":{"content":"b"}}]}`, true},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			var chunk openaisdk.ChatCompletionChunk
			require.NoError(t, json.Unmarshal([]byte(tc.chunk), &chunk))
			assert.Equal(t, tc.want, chatChunkHasOutput(chunk))
		})
	}
}

func TestStreamClock_EndsOnlyWhenEveryChoiceHasFinished(t *testing.T) {
	clock := startStreamClock()
	clock.observe(false, choiceProgress{0: false, 1: false})
	clock.observe(true, choiceProgress{0: true, 1: false})
	assert.True(t, clock.end.IsZero(), "choice 1 is still streaming")

	time.Sleep(5 * time.Millisecond)
	clock.observe(false, choiceProgress{1: true})
	lastFinish := clock.end
	require.False(t, lastFinish.IsZero())

	time.Sleep(5 * time.Millisecond)
	clock.markExhausted()
	assert.Equal(t, lastFinish, clock.end, "the end of the stream does not move a finished call")
	assert.Equal(t, lastFinish.Sub(clock.start), clock.duration())
}

func TestStreamClock_AChoiceThatAppearsLateReopensTheCall(t *testing.T) {
	clock := startStreamClock()
	clock.observe(true, choiceProgress{0: true})
	clock.observe(true, choiceProgress{1: false})

	assert.True(t, clock.end.IsZero())
}

func TestChatStreaming_EndsAtTheLastChoiceToFinish(t *testing.T) {
	r, mock := newTimingClient(t, slowStreamServer(t, twoChoiceChatChunks))

	callStart := time.Now()
	stream, err := r.Chat().Completions().NewStreaming(context.Background(), readmeStreamingParams())
	require.NoError(t, err)
	readThenCloseLate(t, stream, func() bool { return false })

	assertMeasuredLikeTheWallClock(t, singleTimingRecord(t, r, mock), callStart)
}

func assertEndedAtTheLateClose(t *testing.T, record map[string]interface{}) {
	t.Helper()
	duration := time.Duration(record["requestDuration"].(float64)) * time.Millisecond
	assert.GreaterOrEqual(t, duration, wantFirstToken+lateClose, "a choice still streaming keeps the call open until Close()")
}

func TestChatStreaming_OneFinishedChoiceDoesNotEndACallStillStreamingAnother(t *testing.T) {
	r, mock := newTimingClient(t, slowStreamServer(t, twoChoiceChatChunks))

	stream, err := r.Chat().Completions().NewStreaming(context.Background(), readmeStreamingParams())
	require.NoError(t, err)
	readThenCloseLate(t, stream, func() bool { return chunkFinished(stream.Current()) })

	assertEndedAtTheLateClose(t, singleTimingRecord(t, r, mock))
}

func TestTextCompletionStreaming_OneFinishedChoiceDoesNotEndACallStillStreamingAnother(t *testing.T) {
	r, mock := newTimingClient(t, slowStreamServer(t, twoChoiceTextChunks))

	stream, err := r.Completions().NewStreaming(context.Background(), textCompletionParams())
	require.NoError(t, err)
	readThenCloseLate(t, stream, func() bool { return textCompletionFinished(stream.Current()) })

	assertEndedAtTheLateClose(t, singleTimingRecord(t, r, mock))
}

func TestStreaming_WithoutAFinishReasonEndsAtTheEndOfTheStream(t *testing.T) {
	cases := map[string]func(*ReveniumOpenAI) (nextReader, error){
		"chat": func(r *ReveniumOpenAI) (nextReader, error) {
			return r.Chat().Completions().NewStreaming(context.Background(), readmeStreamingParams())
		},
		"text completions": func(r *ReveniumOpenAI) (nextReader, error) {
			return r.Completions().NewStreaming(context.Background(), textCompletionParams())
		},
		"responses": func(r *ReveniumOpenAI) (nextReader, error) {
			return r.Responses().CreateStreaming(context.Background(), responses.ResponseNewParams{Model: "gpt-4.1-nano"})
		},
	}
	fixtures := map[string]slowStreamChunks{
		"chat":             withoutFinish(slowChatChunks),
		"text completions": withoutFinish(slowTextChunks),
		"responses":        withoutFinish(slowResponsesEvents),
	}
	for name, open := range cases {
		t.Run(name, func(t *testing.T) {
			r, mock := newTimingClient(t, slowStreamServer(t, fixtures[name]))

			callStart := time.Now()
			stream, err := open(r)
			require.NoError(t, err)
			readThenCloseLate(t, stream, func() bool { return false })

			assertMeasuredLikeTheWallClock(t, singleTimingRecord(t, r, mock), callStart)
		})
	}
}

func TestStreamClock_ZeroValueIsUsable(t *testing.T) {
	var clock streamClock
	clock.observe(true, choiceProgress{0: true})
	assert.True(t, clock.finished[0])
}
