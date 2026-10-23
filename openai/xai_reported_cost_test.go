package openai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/openai/openai-go/v3/packages/respjson"
	"github.com/revenium/revenium-go-sdk/core/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const xaiUsageWithCost = `{"prompt_tokens":100,"completion_tokens":20,"total_tokens":420,"completion_tokens_details":{"reasoning_tokens":300},"cost_in_usd_ticks":6009000}`

func xaiChatHandler(usage string) http.Handler {
	return jsonResponder(`{"id":"chatcmpl-x","object":"chat.completion","created":1700000000,"model":"grok-4.3",` +
		`"choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}],"usage":` + usage + `}`)
}

func xaiStreamHandler(t *testing.T, usage string) http.Handler {
	return sseResponder(t,
		`{"id":"chatcmpl-x","object":"chat.completion.chunk","created":1700000000,"model":"grok-4.3","choices":[{"index":0,"delta":{"role":"assistant","content":"hi"},"finish_reason":null}]}`,
		`{"id":"chatcmpl-x","object":"chat.completion.chunk","created":1700000000,"model":"grok-4.3","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":`+usage+`}`,
	)
}

func recordedXAIChat(t *testing.T, provider http.Handler, streamed bool) map[string]interface{} {
	t.Helper()
	providerServer := httptest.NewServer(provider)
	defer providerServer.Close()
	mock := testutil.NewMockMeteringServer()
	defer mock.Close()
	r := newXAITestOpenAI(t, providerServer, mock.URL())

	if streamed {
		stream, err := r.Chat().Completions().NewStreaming(context.Background(), chatParams("grok-4.3"))
		require.NoError(t, err)
		for stream.Next() {
		}
		require.NoError(t, stream.Close())
	} else {
		_, err := r.Chat().Completions().New(context.Background(), chatParams("grok-4.3"))
		require.NoError(t, err)
	}
	r.Flush()
	return assertSingleRecordAfterTimeout(t, mock)
}

func TestXAIChat_RecordsTheCostXAIReports(t *testing.T) {
	handlers := map[string]struct {
		handler  func(t *testing.T, usage string) http.Handler
		streamed bool
	}{
		"sync":     {handler: func(_ *testing.T, usage string) http.Handler { return xaiChatHandler(usage) }},
		"streamed": {handler: xaiStreamHandler, streamed: true},
	}
	for name, call := range handlers {
		t.Run(name, func(t *testing.T) {
			record := recordedXAIChat(t, call.handler(t, xaiUsageWithCost), call.streamed)

			assert.InDelta(t, 0.0006009, record["totalCost"], 1e-12)
			assert.NotContains(t, record, "inputTokenCost")
			assert.NotContains(t, record, "outputTokenCost")
			assert.Equal(t, "XAI", record["provider"])
		})
	}
}

func TestXAIChat_WithoutAReportedCostSendsNoCost(t *testing.T) {
	usage := `{"prompt_tokens":100,"completion_tokens":20,"total_tokens":120}`
	for name, streamed := range map[string]bool{"sync": false, "streamed": true} {
		t.Run(name, func(t *testing.T) {
			handler := xaiChatHandler(usage)
			if streamed {
				handler = xaiStreamHandler(t, usage)
			}

			record := recordedXAIChat(t, handler, streamed)

			assert.NotContains(t, record, "totalCost")
			assert.Equal(t, float64(20), record["outputTokenCount"])
		})
	}
}

func TestXAIReportedCostUSD(t *testing.T) {
	field := func(raw string) map[string]respjson.Field {
		return map[string]respjson.Field{xaiCostField: respjson.NewInvalidField(raw)}
	}

	assert.Nil(t, xaiReportedCostUSD(nil))
	assert.Nil(t, xaiReportedCostUSD(field("-1")))
	assert.Nil(t, xaiReportedCostUSD(field(`"6009000"`)))
	assert.Nil(t, xaiReportedCostUSD(field("null")))

	zero := xaiReportedCostUSD(field("0"))
	require.NotNil(t, zero)
	assert.Equal(t, 0.0, *zero)

	cost := xaiReportedCostUSD(field("5881000"))
	require.NotNil(t, cost)
	assert.InDelta(t, 0.0005881, *cost, 1e-12)
}
