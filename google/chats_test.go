package google

import (
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"testing"

	"github.com/revenium/revenium-go-sdk/core/enforcement"
	"github.com/revenium/revenium-go-sdk/core/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/genai"
)

const chatAnswer = `{"candidates":[{"content":{"role":"model","parts":[{"text":"hi"}]},"finishReason":"STOP"}],` +
	`"usageMetadata":{"promptTokenCount":4,"candidatesTokenCount":2,"totalTokenCount":6}}`

type sentContents struct {
	mu     sync.Mutex
	counts []int
}

func (s *sentContents) observe(r *http.Request) {
	var body struct {
		Contents []json.RawMessage `json:"contents"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.counts = append(s.counts, len(body.Contents))
}

func (s *sentContents) perRequest() []int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]int(nil), s.counts...)
}

func newChatStub(t *testing.T) (*stubbedClient, *sentContents) {
	t.Helper()
	sent := &sentContents{}
	c := newStubbedClient(t, ProviderGoogleAI, func(w http.ResponseWriter, r *http.Request) {
		sent.observe(r)
		if isStreamRequest(r) {
			writeSSE(w,
				`{"candidates":[{"content":{"role":"model","parts":[{"text":"h"}]}}]}`,
				`{"candidates":[{"content":{"role":"model","parts":[{"text":"i"}]},"finishReason":"STOP"}],`+
					`"usageMetadata":{"promptTokenCount":8,"candidatesTokenCount":2,"totalTokenCount":10}}`)
			return
		}
		writeJSON(w, http.StatusOK, chatAnswer)
	})
	return c, sent
}

func TestChat_RecordsOneUsageRecordPerTurn(t *testing.T) {
	c, sent := newChatStub(t)
	chat, err := c.client.Chats().Create(asSubscriber("alice@example.com"), "gemini-2.0-flash", nil, nil)
	require.NoError(t, err)

	_, err = chat.SendMessage(asSubscriber("alice@example.com"), genai.Part{Text: "first"})
	require.NoError(t, err)
	_, err = chat.Send(asSubscriber("alice@example.com"), genai.NewPartFromText("second"))
	require.NoError(t, err)
	for _, streamErr := range chat.SendMessageStream(asSubscriber("alice@example.com"), genai.Part{Text: "third"}) {
		require.NoError(t, streamErr)
	}

	assert.Len(t, c.provider.requests(), 3)
	assert.Equal(t, []int{1, 3, 5}, sent.perRequest(), "each turn carries the conversation so far")
	records := c.records(t)
	require.Len(t, records, 3)
	var streamed []map[string]interface{}
	for _, record := range records {
		assert.Equal(t, map[string]interface{}{"email": "alice@example.com"}, record["subscriber"])
		if record["isStreamed"] == true {
			streamed = append(streamed, record)
		}
	}
	require.Len(t, streamed, 1, "records arrive in any order; exactly one turn was streamed")
	assert.Equal(t, float64(8), streamed[0]["inputTokenCount"])
}

func TestChat_KeepsHistoryLikeTheUpstreamChat(t *testing.T) {
	c, _ := newChatStub(t)
	earlier := []*genai.Content{genai.NewContentFromText("before", genai.RoleUser), genai.NewContentFromText("ok", genai.RoleModel)}
	chat, err := c.client.Chats().Create(asSubscriber("alice@example.com"), "gemini-2.0-flash", nil, earlier)
	require.NoError(t, err)

	_, err = chat.SendMessage(asSubscriber("alice@example.com"), genai.Part{Text: "next"})
	require.NoError(t, err)

	history := chat.History(true)
	require.Len(t, history, 4)
	assert.Equal(t, "next", history[2].Parts[0].Text)
	assert.Equal(t, genai.RoleModel, history[3].Role)
	assert.Len(t, chat.History(false), 4)
}

func TestChat_StreamEndingWithAUsageOnlyChunkKeepsTheAnswerInHistory(t *testing.T) {
	sent := &sentContents{}
	c := newStubbedClient(t, ProviderGoogleAI, func(w http.ResponseWriter, r *http.Request) {
		sent.observe(r)
		if isStreamRequest(r) {
			writeSSE(w,
				`{"candidates":[{"content":{"role":"model","parts":[{"text":"hi"}]},"finishReason":"STOP"}]}`,
				`{"usageMetadata":{"promptTokenCount":4,"candidatesTokenCount":2,"totalTokenCount":6}}`)
			return
		}
		writeJSON(w, http.StatusOK, chatAnswer)
	})
	chat, err := c.client.Chats().Create(asSubscriber("alice@example.com"), "gemini-2.0-flash", nil, nil)
	require.NoError(t, err)

	for _, streamErr := range chat.SendMessageStream(asSubscriber("alice@example.com"), genai.Part{Text: "first"}) {
		require.NoError(t, streamErr)
	}
	_, err = chat.SendMessage(asSubscriber("alice@example.com"), genai.Part{Text: "second"})
	require.NoError(t, err)

	assert.Equal(t, []int{1, 3}, sent.perRequest(), "the follow-up carries the streamed answer")
	require.Len(t, chat.History(true), 4)
	assert.Equal(t, "hi", chat.History(true)[1].Parts[0].Text)
	assert.Len(t, c.records(t), 2)
}

func TestChat_RejectsHistoryTheUpstreamChatRejects(t *testing.T) {
	c, _ := newChatStub(t)
	_, err := c.client.Chats().Create(asSubscriber("alice@example.com"), "gemini-2.0-flash", nil,
		[]*genai.Content{{Role: "system", Parts: []*genai.Part{{Text: "x"}}}})
	require.Error(t, err)
}

func TestChat_FailedTurnIsRecordedAndLeavesHistoryUntouched(t *testing.T) {
	c := newStubbedClient(t, ProviderGoogleAI, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusBadRequest, `{"error":{"code":400,"message":"bad","status":"INVALID_ARGUMENT"}}`)
	})
	chat, err := c.client.Chats().Create(asSubscriber("alice@example.com"), "gemini-2.0-flash", nil, nil)
	require.NoError(t, err)

	_, err = chat.SendMessage(asSubscriber("alice@example.com"), genai.Part{Text: "hi"})
	require.Error(t, err)

	assert.Empty(t, chat.History(false))
	record := singleRecord(t, c)
	assert.NotEmpty(t, record["errorReason"])
}

func TestChat_TurnsPassTheCostControlGate(t *testing.T) {
	enforcement.Stop()
	t.Cleanup(enforcement.Stop)
	provider := newGeminiStub(t)
	rules := testutil.NewMockRulesServer()
	t.Cleanup(rules.Close)
	metering := testutil.NewMockMeteringServer()
	t.Cleanup(metering.Close)
	rules.SetPayload("team-a", `{"rules": [{"ruleId": 7, "name": "per-dev", "metricType": "TOTAL_COST", "threshold": 50,
		"currentValue": 61, "breached": true, "shadowMode": false, "action": "BLOCK", "groupBy": "SUBSCRIBER",
		"filters": [{"dimension": "PROVIDER", "operator": "IS", "value": "GOOGLE_AI"}],
		"groupBreakdown": [{"groupValue": "alice@example.com", "currentValue": 61, "breached": true}]}]}`)

	chats := newEnforcedGoogle(t, rules.URL(), metering.URL(), "team-a").Chats()
	chat, err := chats.Create(asSubscriber("alice@example.com"), "gemini-2.0-flash", nil, nil)
	require.NoError(t, err)

	var ece *enforcement.ErrCostLimitExceeded
	_, err = chat.SendMessage(asSubscriber("alice@example.com"), genai.Part{Text: "hi"})
	require.True(t, errors.As(err, &ece), "alice's chat turn should be blocked, got %v", err)
	for _, streamErr := range chat.SendMessageStream(asSubscriber("alice@example.com"), genai.Part{Text: "hi"}) {
		require.True(t, errors.As(streamErr, &ece), "alice's streamed chat turn should be blocked, got %v", streamErr)
	}
	assert.Equal(t, int32(0), provider.hits.Load(), "a blocked turn never reaches Google")
	assert.Empty(t, chat.History(false))
}
