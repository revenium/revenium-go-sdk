package litellm

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/revenium/revenium-go-sdk/core/enforcement"
	"github.com/revenium/revenium-go-sdk/core/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const noModelRoutes = `{"data":[]}`

const teamFastServedByGrok = `{"data":[
	{"model_name":"team-fast","litellm_params":{"model":"xai/grok-4"},"model_info":{"id":"a"}},
	{"model_name":"team-cheap","litellm_params":{"model":"openai/gpt-4.1-nano"},"model_info":{"id":"b"}}]}`

func answeredModelInfo(w http.ResponseWriter, r *http.Request, routes string) bool {
	if r.URL.Path != modelInfoPath {
		return false
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(routes))
	return true
}

type aliasProxy struct {
	server         *httptest.Server
	completions    atomic.Int32
	modelInfoReads atomic.Int32
	modelInfoCode  atomic.Int32
	authorization  atomic.Value
	userAgent      atomic.Value
	accept         atomic.Value
	embeddings     atomic.Int32
}

func newAliasProxy(t *testing.T, routes string, modelInfoStatus int) *aliasProxy {
	t.Helper()
	proxy := &aliasProxy{}
	proxy.modelInfoCode.Store(int32(modelInfoStatus))
	proxy.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == modelInfoPath {
			proxy.modelInfoReads.Add(1)
			proxy.authorization.Store(r.Header.Get("Authorization"))
			proxy.userAgent.Store(r.Header.Get("User-Agent"))
			proxy.accept.Store(r.Header.Get("Accept"))
			if status := int(proxy.modelInfoCode.Load()); status != http.StatusOK {
				w.WriteHeader(status)
				return
			}
			answeredModelInfo(w, r, routes)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/embeddings") {
			proxy.embeddings.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"object":"list","model":"team-fast","data":[{"object":"embedding","embedding":[0.1],"index":0}],"usage":{"prompt_tokens":2,"total_tokens":2}}`))
			return
		}
		proxy.completions.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl-x","object":"chat.completion","created":1700000000,"model":"xai/grok-4",` +
			`"choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}],` +
			`"usage":{"prompt_tokens":3,"completion_tokens":1,"total_tokens":4}}`))
	}))
	t.Cleanup(proxy.server.Close)
	return proxy
}

type aliasSetup struct {
	proxy    *aliasProxy
	rules    *testutil.MockRulesServer
	metering *testutil.MockMeteringServer
	client   *ReveniumLiteLLM
}

func newAliasSetup(t *testing.T, routes string, modelInfoStatus int, rule, teamID string) aliasSetup {
	t.Helper()
	enforcement.Stop()
	t.Cleanup(enforcement.Stop)
	setup := aliasSetup{proxy: newAliasProxy(t, routes, modelInfoStatus), rules: testutil.NewMockRulesServer(), metering: testutil.NewMockMeteringServer()}
	t.Cleanup(setup.rules.Close)
	t.Cleanup(setup.metering.Close)
	setup.rules.SetPayload(enforcementTestTeam, breachedPerPersonRule(rule))
	setup.client = newEnforcedLiteLLM(t, setup.proxy.server.URL, setup.rules.URL(), setup.metering.URL(), teamID)
	return setup
}

const xaiProviderRule = `{"dimension": "PROVIDER", "operator": "IS", "value": "XAI"}`

func TestChatCompletions_XAIRuleBlocksAnAliasTheProxyServesWithGrok(t *testing.T) {
	setup := newAliasSetup(t, teamFastServedByGrok, http.StatusOK, xaiProviderRule, enforcementTestTeam)
	completions := setup.client.Chat().Completions()

	_, err := completions.New(asSubscriber("alice@example.com"), chatRequest("team-fast"))
	var ece *enforcement.ErrCostLimitExceeded
	require.True(t, errors.As(err, &ece), "the XAI rule did not block team-fast: %v", err)

	_, err = completions.NewStreaming(asSubscriber("alice@example.com"), chatRequest("team-fast"))
	require.True(t, errors.As(err, &ece), "the XAI rule did not block a streamed team-fast call: %v", err)

	assert.Equal(t, int32(0), setup.proxy.completions.Load(), "a blocked call reached the provider")
	assert.Equal(t, "Bearer sk-litellm-test", setup.proxy.authorization.Load(), "the model list is read with the proxy key")
	assert.Equal(t, modelInfoUserAgent, setup.proxy.userAgent.Load())
	assert.Equal(t, "application/json", setup.proxy.accept.Load())
}

func TestEmbeddings_AnISNOTCapNamingTheEmbeddingModelExemptsIt(t *testing.T) {
	setup := newAliasSetup(t, noModelRoutes, http.StatusNotFound, `{"dimension": "MODEL", "operator": "IS_NOT", "value": "text-embedding-3-small"}`, enforcementTestTeam)

	_, err := setup.client.Embeddings().New(asSubscriber("alice@example.com"), EmbeddingRequest{Model: "openai/text-embedding-3-small", Input: "hi"})
	require.NoError(t, err, "an embedding is recorded under its requested name, so IS_NOT naming it must exempt it")
	assert.Equal(t, int32(1), setup.proxy.embeddings.Load())
}

func TestChatCompletions_ABareOpenAIReasoningModelFollowsTheDatedNameRules(t *testing.T) {
	setup := newAliasSetup(t, noModelRoutes, http.StatusNotFound, `{"dimension": "MODEL", "operator": "IS", "value": "o3-2025-04-16"}`, enforcementTestTeam)

	_, err := setup.client.Chat().Completions().New(asSubscriber("alice@example.com"), chatRequest("o3"))
	var ece *enforcement.ErrCostLimitExceeded
	require.True(t, errors.As(err, &ece), "a dated MODEL cap did not block the bare o3 call: %v", err)
	assert.Equal(t, int32(0), setup.proxy.completions.Load())
}

func TestChatCompletions_ARouteToAProviderServingExactNamesComparesExactly(t *testing.T) {
	setup := newAliasSetup(t, noModelRoutes, http.StatusNotFound, `{"dimension": "MODEL", "operator": "IS_NOT", "value": "llama3"}`, enforcementTestTeam)

	_, err := setup.client.Chat().Completions().New(asSubscriber("alice@example.com"), chatRequest("ollama/llama3"))
	require.NoError(t, err, "an IS_NOT cap naming the exact Ollama model must exempt it")
	assert.Equal(t, int32(1), setup.proxy.completions.Load())
}

func TestEmbeddings_AnAliasIsJudgedByItsRequestedModelOnly(t *testing.T) {
	setup := newAliasSetup(t, teamFastServedByGrok, http.StatusOK, xaiProviderRule, enforcementTestTeam)

	_, err := setup.client.Embeddings().New(asSubscriber("alice@example.com"), EmbeddingRequest{Model: "team-fast", Input: "hi"})
	require.NoError(t, err, "an embedding alias is recorded under its requested model, so the route must not block it")
	assert.Equal(t, int32(1), setup.proxy.embeddings.Load())
}

func TestChatCompletions_XAIRuleLeavesAliasesServedByOtherProvidersAlone(t *testing.T) {
	setup := newAliasSetup(t, teamFastServedByGrok, http.StatusOK, xaiProviderRule, enforcementTestTeam)

	_, err := setup.client.Chat().Completions().New(asSubscriber("alice@example.com"), chatRequest("team-cheap"))

	require.NoError(t, err)
	assert.Equal(t, int32(1), setup.proxy.completions.Load())
}

func TestChatCompletions_AliasUnderAnUnbreachedRuleIsRecordedAsXAI(t *testing.T) {
	setup := newAliasSetup(t, teamFastServedByGrok, http.StatusOK, xaiProviderRule, enforcementTestTeam)

	_, err := setup.client.Chat().Completions().New(asSubscriber("bob@example.com"), chatRequest("team-fast"))
	require.NoError(t, err)
	setup.client.Flush()

	require.True(t, setup.metering.WaitForPayloads(1, 2*time.Second))
	payload := setup.metering.GetPayloads()[0]
	assert.Equal(t, "XAI", payload["provider"])
	assert.Equal(t, "grok-4", payload["model"])
}

func TestChatCompletions_ModelListIsReadOncePerRefresh(t *testing.T) {
	setup := newAliasSetup(t, teamFastServedByGrok, http.StatusOK, xaiProviderRule, enforcementTestTeam)
	completions := setup.client.Chat().Completions()

	for i := 0; i < 3; i++ {
		_, err := completions.New(asSubscriber("bob@example.com"), chatRequest("team-cheap"))
		require.NoError(t, err)
	}

	assert.Equal(t, int32(1), setup.proxy.modelInfoReads.Load())
}

func TestChatCompletions_UnreadableModelListFallsBackToTheRequestedName(t *testing.T) {
	setup := newAliasSetup(t, "", http.StatusForbidden, xaiProviderRule, enforcementTestTeam)
	completions := setup.client.Chat().Completions()

	_, err := completions.New(asSubscriber("alice@example.com"), chatRequest("team-fast"))
	assert.NoError(t, err, "without the model list the alias is judged by its own name, as before")

	_, err = completions.New(asSubscriber("alice@example.com"), chatRequest("xai/grok-4"))
	var ece *enforcement.ErrCostLimitExceeded
	assert.True(t, errors.As(err, &ece), "a requested name that names the provider is still blocked")
}

func expireModelRoutes(client *ReveniumLiteLLM) {
	client.modelRoutes.mu.Lock()
	defer client.modelRoutes.mu.Unlock()
	client.modelRoutes.expiresAt = time.Time{}
}

func TestChatCompletions_FailedRefreshKeepsTheLastModelListRead(t *testing.T) {
	setup := newAliasSetup(t, teamFastServedByGrok, http.StatusOK, xaiProviderRule, enforcementTestTeam)
	completions := setup.client.Chat().Completions()
	_, err := completions.New(asSubscriber("bob@example.com"), chatRequest("team-cheap"))
	require.NoError(t, err)

	setup.proxy.modelInfoCode.Store(http.StatusInternalServerError)
	expireModelRoutes(setup.client)
	_, err = completions.New(asSubscriber("alice@example.com"), chatRequest("team-fast"))

	var ece *enforcement.ErrCostLimitExceeded
	assert.True(t, errors.As(err, &ece), "a failed refresh dropped the alias route read before: %v", err)
	assert.Equal(t, int32(2), setup.proxy.modelInfoReads.Load(), "the expired list was not read again")
}

func TestModelRoutes_CallerThatGaveUpDoesNotMarkTheListUnreadable(t *testing.T) {
	setup := newAliasSetup(t, teamFastServedByGrok, http.StatusOK, xaiProviderRule, enforcementTestTeam)
	expired, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()

	setup.client.modelRoutes.lookup(expired, setup.client, "team-fast")
	routes := setup.client.modelRoutes.lookup(context.Background(), setup.client, "team-fast")

	assert.Equal(t, []string{"xai/grok-4"}, routes)
	assert.Equal(t, int32(1), setup.proxy.modelInfoReads.Load(), "the expired caller's read was cached as a failure")
}

func newStalledModelInfoClient(t *testing.T, release <-chan struct{}, reads *atomic.Int32) *ReveniumLiteLLM {
	t.Helper()
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reads.Add(1)
		<-release
		answeredModelInfo(w, r, teamFastServedByGrok)
	}))
	t.Cleanup(proxy.Close)
	rules := testutil.NewMockRulesServer()
	t.Cleanup(rules.Close)
	metering := testutil.NewMockMeteringServer()
	t.Cleanup(metering.Close)
	return newEnforcedLiteLLM(t, proxy.URL, rules.URL(), metering.URL(), enforcementTestTeam)
}

func TestModelRoutes_ShortDeadlineCallerStopsWaitingForAStalledRead(t *testing.T) {
	enforcement.Stop()
	t.Cleanup(enforcement.Stop)
	release := make(chan struct{})
	var reads atomic.Int32
	client := newStalledModelInfoClient(t, release, &reads)
	t.Cleanup(func() { close(release) })

	short, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	started := time.Now()
	routes := client.modelRoutes.lookup(short, client, "team-fast")

	assert.Empty(t, routes, "a caller that stopped waiting is judged by the requested name")
	assert.Less(t, time.Since(started), time.Second, "the caller waited past its own deadline")

	release <- struct{}{}
	assert.Equal(t, []string{"xai/grok-4"}, client.modelRoutes.lookup(context.Background(), client, "team-fast"))
	assert.Equal(t, int32(1), reads.Load(), "the shared read was started again")
}

func TestFetchModelRoutes_RefusedListIsAProviderError(t *testing.T) {
	setup := newAliasSetup(t, "", http.StatusForbidden, xaiProviderRule, enforcementTestTeam)

	_, err := setup.client.fetchModelRoutes(context.Background())

	var reveniumErr *core.ReveniumError
	require.True(t, errors.As(err, &reveniumErr), "got %T", err)
	assert.Equal(t, core.ErrorTypeProvider, reveniumErr.Type)
}

func TestChatCompletions_ModelListIsNotReadWithoutCostControls(t *testing.T) {
	setup := newAliasSetup(t, teamFastServedByGrok, http.StatusOK, xaiProviderRule, "")

	_, err := setup.client.Chat().Completions().New(asSubscriber("alice@example.com"), chatRequest("team-fast"))

	require.NoError(t, err)
	assert.Equal(t, int32(0), setup.proxy.modelInfoReads.Load())
}

func TestModelInfoAnswer_GroupsDeploymentsByRequestedNameAndMapsEachID(t *testing.T) {
	answer := modelInfoAnswer{}
	require.NoError(t, json.Unmarshal([]byte(`{"data":[
		{"model_name":"team-fast","litellm_params":{"model":"xai/grok-4"}},
		{"model_name":"team-fast","litellm_params":{"model":"openai/gpt-4.1-nano"},"model_info":{"id":"nano-1"}},
		{"model_name":"team-fast","litellm_params":{"model":"xai/grok-4"}},
		{"model_name":"","litellm_params":{"model":"xai/grok-3"},"model_info":{"id":"grok-3"}},
		{"model_name":"no-route","litellm_params":{},"model_info":{"id":"empty"}}]}`), &answer))

	list := answer.modelList()
	assert.Equal(t, map[string][]string{"team-fast": {"xai/grok-4", "openai/gpt-4.1-nano"}}, list.routes)
	assert.Equal(t, map[string]string{"nano-1": "openai/gpt-4.1-nano", "grok-3": "xai/grok-3"}, list.deployments)
}
