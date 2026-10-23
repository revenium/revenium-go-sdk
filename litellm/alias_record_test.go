package litellm

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/revenium/revenium-go-sdk/core/enforcement"
	"github.com/revenium/revenium-go-sdk/core/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const teamFastDeploymentID = "025db318073af990"

const teamFastServedByNano = `{"data":[
	{"model_name":"team-fast","litellm_params":{"model":"openai/gpt-4.1-nano"},"model_info":{"id":"` + teamFastDeploymentID + `"}},
	{"model_name":"team-mixed","litellm_params":{"model":"openai/gpt-4.1-nano"},"model_info":{"id":"m1"}},
	{"model_name":"team-mixed","litellm_params":{"model":"anthropic/claude-haiku-4-5"},"model_info":{"id":"m2"}}]}`

// liteLLM105Headers are the headers LiteLLM 1.105 sets on a non-streamed answer to the team-fast alias.
func liteLLM105Headers() map[string]string {
	return map[string]string{
		"x-litellm-model-name":     "openai/gpt-4.1-nano",
		"x-litellm-model-id":       teamFastDeploymentID,
		"x-litellm-model-group":    "team-fast",
		"x-litellm-response-cost":  "1.2999999999999998e-06",
		"x-litellm-model-api-base": "https://api.openai.com",
	}
}

type echoingAliasProxy struct {
	answerModel string
	headers     map[string]string
	usage       string
}

func (p echoingAliasProxy) start(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if answeredModelInfo(w, r, teamFastServedByNano) {
			return
		}
		for name, value := range p.headers {
			w.Header().Set(name, value)
		}
		var body ChatCompletionRequest
		if json.NewDecoder(r.Body).Decode(&body) == nil && body.Stream {
			w.Header().Set("Content-Type", "text/event-stream")
			for _, line := range []string{
				`data: {"id":"chatcmpl-x","object":"chat.completion.chunk","created":1700000000,"model":"` + p.answerModel + `","choices":[{"index":0,"delta":{"role":"assistant","content":"Ok"},"finish_reason":null}]}`,
				`data: {"id":"chatcmpl-x","object":"chat.completion.chunk","created":1700000000,"model":"` + p.answerModel + `","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":` + p.usage + `}`,
				"data: [DONE]",
			} {
				_, _ = w.Write([]byte(line + "\n\n"))
			}
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl-x","object":"chat.completion","created":1700000000,"model":"` + p.answerModel + `",` +
			`"choices":[{"index":0,"message":{"role":"assistant","content":"Ok"},"finish_reason":"stop"}],"usage":` + p.usage + `}`))
	}))
	t.Cleanup(server.Close)
	return server
}

const nanoUsage = `{"prompt_tokens":9,"completion_tokens":1,"total_tokens":10}`

func recordedAliasCall(t *testing.T, client *ReveniumLiteLLM, mock *testutil.MockMeteringServer, requested string, streamed bool) map[string]interface{} {
	t.Helper()
	completions := client.Chat().Completions()
	if !streamed {
		_, err := completions.New(context.Background(), chatRequest(requested))
		require.NoError(t, err)
		return singlePayload(t, client, mock)
	}
	stream, err := completions.NewStreaming(context.Background(), chatRequest(requested))
	require.NoError(t, err)
	for stream.Next() {
	}
	require.NoError(t, stream.Close())
	return singlePayload(t, client, mock)
}

func recordedThroughProxy(t *testing.T, proxy echoingAliasProxy, requested string, streamed bool) map[string]interface{} {
	t.Helper()
	mock := testutil.NewMockMeteringServer()
	t.Cleanup(mock.Close)
	client := newTestLiteLLM(t, proxy.start(t).URL, mock.URL())
	t.Cleanup(func() { _ = client.Close() })
	return recordedAliasCall(t, client, mock, requested, streamed)
}

var deliveryModes = map[string]bool{"non-streaming": false, "streaming": true}

func TestChatCompletions_AnAliasIsRecordedUnderTheDeploymentTheProxyServed(t *testing.T) {
	for name, streamed := range deliveryModes {
		t.Run(name, func(t *testing.T) {
			proxy := echoingAliasProxy{answerModel: "team-fast", headers: liteLLM105Headers(), usage: nanoUsage}

			payload := recordedThroughProxy(t, proxy, "team-fast", streamed)

			assert.Equal(t, "gpt-4.1-nano", payload["model"])
			assert.Equal(t, "OpenAI", payload["provider"])
			assert.Equal(t, "OPENAI", payload["modelSource"])
		})
	}
}

func TestChatCompletions_RecordsTheCostFromTheProxyHeaderWhenUsageHasNone(t *testing.T) {
	for name, streamed := range deliveryModes {
		t.Run(name, func(t *testing.T) {
			proxy := echoingAliasProxy{answerModel: "team-fast", headers: liteLLM105Headers(), usage: nanoUsage}

			payload := recordedThroughProxy(t, proxy, "team-fast", streamed)

			assert.InDelta(t, 1.3e-06, payload["totalCost"], 1e-15)
		})
	}
}

func TestChatCompletions_UsageCostWinsOverTheProxyHeaderCost(t *testing.T) {
	for name, streamed := range deliveryModes {
		t.Run(name, func(t *testing.T) {
			proxy := echoingAliasProxy{answerModel: "team-fast", headers: liteLLM105Headers(),
				usage: `{"prompt_tokens":9,"completion_tokens":1,"total_tokens":10,"cost":0.0042}`}

			payload := recordedThroughProxy(t, proxy, "team-fast", streamed)

			assert.InDelta(t, 0.0042, payload["totalCost"], 1e-12)
		})
	}
}

func TestChatCompletions_IgnoresAHeaderCostThatIsNotAPrice(t *testing.T) {
	for _, headerCost := range []string{"0.0", "None", "-1", "NaN", "not-a-number", ""} {
		t.Run(headerCost, func(t *testing.T) {
			headers := liteLLM105Headers()
			headers["x-litellm-response-cost"] = headerCost
			proxy := echoingAliasProxy{answerModel: "team-fast", headers: headers, usage: nanoUsage}

			payload := recordedThroughProxy(t, proxy, "team-fast", false)

			assert.NotContains(t, payload, "totalCost")
		})
	}
}

func TestChatCompletions_KeepsAProviderModelTheProxyPassedThrough(t *testing.T) {
	for name, streamed := range deliveryModes {
		t.Run(name, func(t *testing.T) {
			proxy := echoingAliasProxy{answerModel: "gpt-4.1-nano-2025-04-14", headers: liteLLM105Headers(), usage: nanoUsage}

			payload := recordedThroughProxy(t, proxy, "team-fast", streamed)

			assert.Equal(t, "gpt-4.1-nano-2025-04-14", payload["model"])
			assert.Equal(t, "OpenAI", payload["provider"])
		})
	}
}

func TestChatCompletions_WithoutDeploymentHeadersRecordsTheAnswerModel(t *testing.T) {
	for name, streamed := range deliveryModes {
		t.Run(name, func(t *testing.T) {
			proxy := echoingAliasProxy{answerModel: "team-fast", usage: nanoUsage}

			payload := recordedThroughProxy(t, proxy, "team-fast", streamed)

			assert.Equal(t, "team-fast", payload["model"])
			assert.Equal(t, "LiteLLM", payload["provider"])
		})
	}
}

// The model list is read only when cost controls are on, so these calls run with a team and no rules.
func recordedWithModelList(t *testing.T, proxy echoingAliasProxy, requested string, streamed bool) map[string]interface{} {
	t.Helper()
	enforcement.Stop()
	t.Cleanup(enforcement.Stop)
	rules := testutil.NewMockRulesServer()
	t.Cleanup(rules.Close)
	mock := testutil.NewMockMeteringServer()
	t.Cleanup(mock.Close)
	client := newEnforcedLiteLLM(t, proxy.start(t).URL, rules.URL(), mock.URL(), enforcementTestTeam)
	return recordedAliasCall(t, client, mock, requested, streamed)
}

func TestChatCompletions_ResolvesTheServedDeploymentFromTheCachedModelList(t *testing.T) {
	cases := map[string]struct {
		requested string
		headers   map[string]string
		wantModel string
	}{
		"by deployment id":              {"team-fast", map[string]string{"x-litellm-model-id": teamFastDeploymentID}, "gpt-4.1-nano"},
		"by a single-route model group": {"team-fast", map[string]string{"x-litellm-model-group": "team-fast"}, "gpt-4.1-nano"},
		"not by an unknown id":          {"team-fast", map[string]string{"x-litellm-model-id": "unknown"}, "team-fast"},
		"not by a multi-route group":    {"team-mixed", map[string]string{"x-litellm-model-group": "team-mixed"}, "team-mixed"},
	}
	for name, tc := range cases {
		for mode, streamed := range deliveryModes {
			t.Run(name+"/"+mode, func(t *testing.T) {
				proxy := echoingAliasProxy{answerModel: tc.requested, headers: tc.headers, usage: nanoUsage}

				payload := recordedWithModelList(t, proxy, tc.requested, streamed)

				assert.Equal(t, tc.wantModel, payload["model"])
			})
		}
	}
}

func TestChatCompletions_AnOpenAICapBlocksTheAliasItsRecordsCountToward(t *testing.T) {
	enforcement.Stop()
	t.Cleanup(enforcement.Stop)
	rules := testutil.NewMockRulesServer()
	t.Cleanup(rules.Close)
	rules.SetPayload(enforcementTestTeam, breachedPerPersonRule(`{"dimension": "PROVIDER", "operator": "IS", "value": "OPENAI"}`))
	mock := testutil.NewMockMeteringServer()
	t.Cleanup(mock.Close)
	proxy := echoingAliasProxy{answerModel: "team-fast", headers: liteLLM105Headers(), usage: nanoUsage}
	client := newEnforcedLiteLLM(t, proxy.start(t).URL, rules.URL(), mock.URL(), enforcementTestTeam)
	completions := client.Chat().Completions()

	_, err := completions.New(asSubscriber("alice@example.com"), chatRequest("team-fast"))
	var ece *enforcement.ErrCostLimitExceeded
	require.True(t, errors.As(err, &ece), "the OPENAI cap did not block team-fast: %v", err)

	_, err = completions.New(asSubscriber("bob@example.com"), chatRequest("team-fast"))
	require.NoError(t, err)
	payload := singlePayload(t, client, mock)
	assert.Equal(t, "OPENAI", payload["modelSource"], "the record of an allowed call must count toward the cap that blocked alice")
	assert.Equal(t, "OpenAI", payload["provider"])
	assert.Equal(t, "gpt-4.1-nano", payload["model"])
}
