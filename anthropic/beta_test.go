package anthropic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"

	anthropicsdk "github.com/anthropics/anthropic-sdk-go"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/revenium/revenium-go-sdk/core/enforcement"
	"github.com/revenium/revenium-go-sdk/core/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	betaToolUseMessage = `{"id":"msg_1","type":"message","role":"assistant","model":"claude-haiku-4-5",` +
		`"content":[{"type":"tool_use","id":"toolu_1","name":"lookup","input":{}}],"stop_reason":"tool_use",` +
		`"usage":{"input_tokens":20,"output_tokens":7}}`
	betaFinalMessage = `{"id":"msg_2","type":"message","role":"assistant","model":"claude-haiku-4-5",` +
		`"content":[{"type":"text","text":"done"}],"stop_reason":"end_turn",` +
		`"usage":{"input_tokens":30,"output_tokens":3,"cache_read_input_tokens":4}}`
)

type betaProviderStub struct {
	server  *httptest.Server
	hits    atomic.Int32
	mu      sync.Mutex
	queries []string
	answers []string
}

func newBetaProviderStub(t *testing.T, answers ...string) *betaProviderStub {
	t.Helper()
	stub := &betaProviderStub{answers: answers}
	stub.server = httptest.NewServer(http.HandlerFunc(stub.serve))
	t.Cleanup(stub.server.Close)
	t.Setenv("ANTHROPIC_BASE_URL", stub.server.URL)
	return stub
}

func (s *betaProviderStub) serve(w http.ResponseWriter, r *http.Request) {
	call := int(s.hits.Add(1)) - 1
	s.mu.Lock()
	s.queries = append(s.queries, r.URL.RawQuery)
	s.mu.Unlock()

	answer := s.answers[min(call, len(s.answers)-1)]
	var body struct {
		Stream bool `json:"stream"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	if body.Stream {
		writeMessageAsSSE(w, answer)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(answer))
}

func writeMessageAsSSE(w http.ResponseWriter, answer string) {
	var message struct {
		ID         string            `json:"id"`
		Model      string            `json:"model"`
		Content    []json.RawMessage `json:"content"`
		StopReason string            `json:"stop_reason"`
		Usage      map[string]int    `json:"usage"`
	}
	_ = json.Unmarshal([]byte(answer), &message)
	start, _ := json.Marshal(map[string]interface{}{"type": "message_start", "message": map[string]interface{}{
		"id": message.ID, "type": "message", "role": "assistant", "model": message.Model, "content": []string{},
		"usage": map[string]int{"input_tokens": message.Usage["input_tokens"], "output_tokens": 1},
	}})
	w.Header().Set("Content-Type", "text/event-stream")
	fmt.Fprintf(w, "event: message_start\ndata: %s\n\n", start)
	for i, block := range message.Content {
		fmt.Fprintf(w, "event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":%d,\"content_block\":%s}\n\n", i, block)
		fmt.Fprintf(w, "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":%d,\"delta\":{\"type\":\"text_delta\",\"text\":\"\"}}\n\n", i)
		fmt.Fprintf(w, "event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":%d}\n\n", i)
	}
	fmt.Fprintf(w, "event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":%q},\"usage\":{\"output_tokens\":%d}}\n\n",
		message.StopReason, message.Usage["output_tokens"])
	fmt.Fprint(w, "event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
}

type lookupTool struct {
	calls atomic.Int32
}

func (l *lookupTool) Name() string        { return "lookup" }
func (l *lookupTool) Description() string { return "looks something up" }
func (l *lookupTool) InputSchema() anthropicsdk.BetaToolInputSchemaParam {
	return anthropicsdk.BetaToolInputSchemaParam{Properties: map[string]interface{}{}}
}
func (l *lookupTool) Execute(context.Context, json.RawMessage) ([]anthropicsdk.BetaToolResultBlockParamContentUnion, error) {
	l.calls.Add(1)
	return []anthropicsdk.BetaToolResultBlockParamContentUnion{{OfText: &anthropicsdk.BetaTextBlockParam{Text: "42"}}}, nil
}

func newMeteredAnthropic(t *testing.T) (*ReveniumAnthropic, *testutil.MockMeteringServer) {
	t.Helper()
	mock := testutil.NewMockMeteringServer()
	t.Cleanup(mock.Close)
	r, err := NewReveniumAnthropic(&Config{
		AnthropicAPIKey: "sk-ant-test",
		BedrockDisabled: true,
		Revenium:        &core.ReveniumConfig{APIKey: "hak_test_key_123", BaseURL: mock.URL()},
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = r.Close() })
	return r, mock
}

func recordsAfterFlush(r *ReveniumAnthropic, mock *testutil.MockMeteringServer) []map[string]interface{} {
	r.Flush()
	return mock.GetPayloads()
}

func betaParams() anthropicsdk.BetaMessageNewParams {
	return anthropicsdk.BetaMessageNewParams{
		Model:     "claude-haiku-4-5",
		MaxTokens: 16,
		Messages:  []anthropicsdk.BetaMessageParam{anthropicsdk.NewBetaUserMessage(anthropicsdk.NewBetaTextBlock("hi"))},
	}
}

func runnerParams() anthropicsdk.BetaToolRunnerParams {
	return anthropicsdk.BetaToolRunnerParams{BetaMessageNewParams: betaParams()}
}

func TestBetaCreateMessage_RecordsOneUsageRecordPerCall(t *testing.T) {
	provider := newBetaProviderStub(t, betaFinalMessage)
	r, mock := newMeteredAnthropic(t)
	ctx := asSubscriber("alice@example.com")

	for range 2 {
		_, err := r.BetaMessages().CreateMessage(ctx, betaParams())
		require.NoError(t, err)
	}

	assert.Equal(t, int32(2), provider.hits.Load())
	assert.Equal(t, []string{"beta=true", "beta=true"}, provider.queries)
	records := recordsAfterFlush(r, mock)
	require.Len(t, records, 2)
	assert.Equal(t, float64(30), records[0]["inputTokenCount"])
	assert.Equal(t, float64(3), records[0]["outputTokenCount"])
	assert.Equal(t, float64(4), records[0]["cacheReadTokenCount"])
	assert.Equal(t, "Anthropic", records[0]["provider"])
	assert.Equal(t, map[string]interface{}{"email": "alice@example.com"}, records[0]["subscriber"])
}

func TestBetaCreateMessageStream_RecordsTheStreamedCallOnce(t *testing.T) {
	provider := newBetaProviderStub(t, betaFinalMessage)
	r, mock := newMeteredAnthropic(t)

	stream, err := r.BetaMessages().CreateMessageStream(asSubscriber("alice@example.com"), betaParams())
	require.NoError(t, err)
	for stream.Next() {
		_ = stream.Current()
	}
	require.NoError(t, stream.Err())
	require.NoError(t, stream.Close())
	require.NoError(t, stream.Close())

	assert.Equal(t, int32(1), provider.hits.Load())
	records := recordsAfterFlush(r, mock)
	require.Len(t, records, 1)
	assert.Equal(t, true, records[0]["isStreamed"])
	assert.Equal(t, float64(30), records[0]["inputTokenCount"])
	assert.Equal(t, float64(3), records[0]["outputTokenCount"])
	assert.Equal(t, "END", records[0]["stopReason"])
}

func TestBetaToolRunner_RecordsEveryModelTurn(t *testing.T) {
	provider := newBetaProviderStub(t, betaToolUseMessage, betaFinalMessage)
	r, mock := newMeteredAnthropic(t)
	tool := &lookupTool{}

	runner := r.BetaMessages().NewToolRunner([]anthropicsdk.BetaTool{tool}, runnerParams())
	final, err := runner.RunToCompletion(asSubscriber("alice@example.com"))
	require.NoError(t, err)

	require.NotNil(t, final)
	assert.Equal(t, anthropicsdk.BetaStopReasonEndTurn, final.StopReason)
	assert.Equal(t, int32(1), tool.calls.Load())
	assert.Equal(t, int32(2), provider.hits.Load())
	records := recordsAfterFlush(r, mock)
	require.Len(t, records, 2)
	assert.Equal(t, float64(20), records[0]["inputTokenCount"])
	assert.Equal(t, float64(30), records[1]["inputTokenCount"])
	assert.NoError(t, runner.Err())
}

func TestBetaToolRunner_AllRecordsEveryModelTurn(t *testing.T) {
	provider := newBetaProviderStub(t, betaToolUseMessage, betaFinalMessage)
	r, mock := newMeteredAnthropic(t)

	runner := r.BetaMessages().NewToolRunner([]anthropicsdk.BetaTool{&lookupTool{}}, runnerParams())
	turns := 0
	for _, err := range runner.All(asSubscriber("alice@example.com")) {
		require.NoError(t, err)
		turns++
	}

	assert.Equal(t, 2, turns)
	assert.Equal(t, int32(2), provider.hits.Load())
	assert.Len(t, recordsAfterFlush(r, mock), 2)
}

func TestBetaToolRunnerStreaming_RecordsEveryModelTurn(t *testing.T) {
	provider := newBetaProviderStub(t, betaToolUseMessage, betaFinalMessage)
	r, mock := newMeteredAnthropic(t)
	tool := &lookupTool{}

	runner := r.BetaMessages().NewToolRunnerStreaming([]anthropicsdk.BetaTool{tool}, runnerParams())
	for events, err := range runner.AllStreaming(asSubscriber("alice@example.com")) {
		require.NoError(t, err)
		for _, eventErr := range events {
			require.NoError(t, eventErr)
		}
	}

	assert.True(t, runner.IsCompleted())
	assert.Equal(t, int32(1), tool.calls.Load())
	assert.Equal(t, int32(2), provider.hits.Load())
	records := recordsAfterFlush(r, mock)
	require.Len(t, records, 2)
	assert.Equal(t, true, records[0]["isStreamed"])
	assert.Equal(t, float64(7), records[0]["outputTokenCount"])
	assert.Equal(t, float64(30), records[1]["inputTokenCount"])
}

func TestBetaSurfaces_PassTheCostControlGate(t *testing.T) {
	enforcement.Stop()
	t.Cleanup(enforcement.Stop)
	provider := newBetaProviderStub(t, betaFinalMessage)
	rules := testutil.NewMockRulesServer()
	t.Cleanup(rules.Close)
	metering := testutil.NewMockMeteringServer()
	t.Cleanup(metering.Close)
	rules.SetPayload(enforcementTestTeam, `{"rules": [{"ruleId": 7, "name": "per-dev", "metricType": "TOTAL_COST", "threshold": 50,
		"currentValue": 61, "breached": true, "shadowMode": false, "action": "BLOCK", "groupBy": "SUBSCRIBER",
		"filters": [{"dimension": "PROVIDER", "operator": "IS", "value": "Anthropic"}],
		"groupBreakdown": [{"groupValue": "alice@example.com", "currentValue": 61, "breached": true}]}]}`)

	beta := newEnforcedAnthropic(t, provider.server.URL, rules.URL(), metering.URL(), enforcementTestTeam).BetaMessages()
	alice := asSubscriber("alice@example.com")
	var ece *enforcement.ErrCostLimitExceeded

	_, err := beta.CreateMessage(alice, betaParams())
	require.True(t, errors.As(err, &ece), "alice's beta message should be blocked, got %v", err)
	_, err = beta.CreateMessageStream(alice, betaParams())
	require.True(t, errors.As(err, &ece), "alice's beta stream should be blocked, got %v", err)

	runner := beta.NewToolRunner([]anthropicsdk.BetaTool{&lookupTool{}}, runnerParams())
	_, err = runner.RunToCompletion(alice)
	require.True(t, errors.As(err, &ece), "alice's tool runner should be blocked, got %v", err)
	require.True(t, errors.As(runner.Err(), &ece))

	streaming := beta.NewToolRunnerStreaming([]anthropicsdk.BetaTool{&lookupTool{}}, runnerParams())
	for _, eventErr := range streaming.NextStreaming(alice) {
		require.True(t, errors.As(eventErr, &ece), "alice's streaming tool runner should be blocked, got %v", eventErr)
	}
	require.True(t, errors.As(streaming.Err(), &ece))

	assert.Equal(t, int32(0), provider.hits.Load(), "a blocked beta call never reaches Anthropic")

	_, err = beta.CreateMessage(asSubscriber("bob@example.com"), betaParams())
	require.NoError(t, err)
	assert.Equal(t, int32(1), provider.hits.Load())
}

func TestBetaToolRunnerStreaming_AllStreamingStopsAtABlockedTurn(t *testing.T) {
	enforcement.Stop()
	t.Cleanup(enforcement.Stop)
	provider := newBetaProviderStub(t, betaFinalMessage)
	rules := testutil.NewMockRulesServer()
	t.Cleanup(rules.Close)
	metering := testutil.NewMockMeteringServer()
	t.Cleanup(metering.Close)
	rules.SetPayload(enforcementTestTeam, `{"rules": [{"ruleId": 9, "threshold": 10, "currentValue": 12, "breached": true,
		"shadowMode": false, "action": "BLOCK", "filters": []}]}`)

	beta := newEnforcedAnthropic(t, provider.server.URL, rules.URL(), metering.URL(), enforcementTestTeam).BetaMessages()
	runner := beta.NewToolRunnerStreaming([]anthropicsdk.BetaTool{&lookupTool{}}, runnerParams())

	var ece *enforcement.ErrCostLimitExceeded
	turns := 0
	var outerErr error
	for events, err := range runner.AllStreaming(asSubscriber("alice@example.com")) {
		if err != nil {
			outerErr = err
			continue
		}
		turns++
		require.Less(t, turns, 3, "a blocked turn must not be offered again")
		for _, eventErr := range events {
			require.True(t, errors.As(eventErr, &ece), "the turn should report the block, got %v", eventErr)
		}
	}

	assert.Equal(t, 1, turns)
	require.True(t, errors.As(outerErr, &ece), "AllStreaming should end with the block, got %v", outerErr)
	assert.Equal(t, int32(0), provider.hits.Load())
}

func TestBetaToolRunner_AllStopsAtABlockedTurn(t *testing.T) {
	enforcement.Stop()
	t.Cleanup(enforcement.Stop)
	provider := newBetaProviderStub(t, betaFinalMessage)
	rules := testutil.NewMockRulesServer()
	t.Cleanup(rules.Close)
	metering := testutil.NewMockMeteringServer()
	t.Cleanup(metering.Close)
	rules.SetPayload(enforcementTestTeam, `{"rules": [{"ruleId": 9, "threshold": 10, "currentValue": 12, "breached": true,
		"shadowMode": false, "action": "BLOCK", "filters": []}]}`)

	beta := newEnforcedAnthropic(t, provider.server.URL, rules.URL(), metering.URL(), enforcementTestTeam).BetaMessages()
	runner := beta.NewToolRunner([]anthropicsdk.BetaTool{&lookupTool{}}, runnerParams())

	var ece *enforcement.ErrCostLimitExceeded
	results := 0
	for message, err := range runner.All(asSubscriber("alice@example.com")) {
		results++
		require.Less(t, results, 3, "a blocked turn must not be offered again")
		assert.Nil(t, message)
		require.True(t, errors.As(err, &ece), "All should report the block, got %v", err)
	}

	assert.Equal(t, 1, results)
	assert.Equal(t, int32(0), provider.hits.Load())
}

type runnerProgressStub struct {
	completed  bool
	iterations int
	last       *anthropicsdk.BetaMessage
}

func (p runnerProgressStub) IsCompleted() bool                      { return p.completed }
func (p runnerProgressStub) IterationCount() int                    { return p.iterations }
func (p runnerProgressStub) LastMessage() *anthropicsdk.BetaMessage { return p.last }

func decodeBetaMessage(t *testing.T, raw string) *anthropicsdk.BetaMessage {
	t.Helper()
	var message anthropicsdk.BetaMessage
	require.NoError(t, json.Unmarshal([]byte(raw), &message))
	return &message
}

func TestRunnerWillCallModel_OnlyWhenTheRunnerSendsAnotherRequest(t *testing.T) {
	toolUse := decodeBetaMessage(t, betaToolUseMessage)
	final := decodeBetaMessage(t, betaFinalMessage)
	paused := decodeBetaMessage(t, `{"stop_reason":"pause_turn","content":[]}`)
	toolUseWithoutCalls := decodeBetaMessage(t, `{"stop_reason":"tool_use","content":[{"type":"text","text":"x"}]}`)

	cases := []struct {
		name          string
		progress      runnerProgressStub
		maxIterations int
		want          bool
	}{
		{"first turn", runnerProgressStub{}, 0, true},
		{"tool calls to answer", runnerProgressStub{iterations: 1, last: toolUse}, 0, true},
		{"paused turn to resume", runnerProgressStub{iterations: 1, last: paused}, 0, true},
		{"final answer", runnerProgressStub{iterations: 2, last: final}, 0, false},
		{"tool_use stop without calls", runnerProgressStub{iterations: 1, last: toolUseWithoutCalls}, 0, false},
		{"iteration limit reached", runnerProgressStub{iterations: 2, last: toolUse}, 2, false},
		{"completed", runnerProgressStub{completed: true, last: toolUse}, 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, runnerWillCallModel(tc.progress, tc.maxIterations))
		})
	}
}
