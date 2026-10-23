package litellm

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

	"github.com/revenium/revenium-go-sdk/core/enforcement"
	"github.com/revenium/revenium-go-sdk/core/testutil"
	"github.com/stretchr/testify/assert"
)

// attributingProxy answers every call and names, in its response headers,
// the person it holds for the request's `user` value.
type attributingProxy struct {
	server *httptest.Server
	hits   atomic.Int32
	mu     sync.Mutex
	people map[string]string
	status int
}

func newAttributingProxyStub(t *testing.T) *attributingProxy {
	t.Helper()
	proxy := &attributingProxy{people: map[string]string{}, status: http.StatusOK}
	proxy.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if answeredModelInfo(w, r, noModelRoutes) {
			return
		}
		proxy.hits.Add(1)
		var body struct {
			User string `json:"user"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		proxy.mu.Lock()
		email, status := proxy.people[body.User], proxy.status
		proxy.mu.Unlock()
		if email != "" {
			w.Header().Set("X-Revenium-Subscriber-Email", email)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if r.URL.Path == "/embeddings" {
			_, _ = w.Write([]byte(`{"object":"list","data":[{"object":"embedding","embedding":[0.1],"index":0}],"model":"openai/text-embedding-3-small","usage":{"prompt_tokens":1,"total_tokens":1}}`))
			return
		}
		_, _ = w.Write([]byte(attributedChatBody))
	}))
	t.Cleanup(proxy.server.Close)
	return proxy
}

func (p *attributingProxy) names(user, email string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.people[user] = email
}

func (p *attributingProxy) answers(status int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.status = status
}

func chatAs(user string) ChatCompletionRequest {
	req := chatRequest("openai/gpt-4.1-nano")
	req.User = user
	return req
}

func assertBlocked(t *testing.T, proxy *attributingProxy, call func() error, who string) {
	t.Helper()
	before := proxy.hits.Load()
	var ece *enforcement.ErrCostLimitExceeded
	assert.True(t, errors.As(call(), &ece), "%s should be blocked", who)
	assert.Equal(t, before, proxy.hits.Load(), "%s reached the proxy", who)
}

func assertReachesProxy(t *testing.T, proxy *attributingProxy, call func() error, who string) {
	t.Helper()
	before := proxy.hits.Load()
	_ = call()
	assert.Equal(t, before+1, proxy.hits.Load(), "%s did not reach the proxy", who)
}

func newHeaderAttributedClient(t *testing.T) (*attributingProxy, *ReveniumLiteLLM) {
	t.Helper()
	enforcement.Stop()
	t.Cleanup(enforcement.Stop)
	proxy := newAttributingProxyStub(t)
	rules := testutil.NewMockRulesServer()
	t.Cleanup(rules.Close)
	metering := testutil.NewMockMeteringServer()
	t.Cleanup(metering.Close)
	rules.SetPayload(enforcementTestTeam, breachedPerPersonRule(`{"dimension": "PROVIDER", "operator": "IS", "value": "OpenAI"}`))
	return proxy, newEnforcedLiteLLM(t, proxy.server.URL, rules.URL(), metering.URL(), enforcementTestTeam)
}

func TestCostControls_APersonTheProxyNamesOnlyInResponseHeadersIsChecked(t *testing.T) {
	proxy, client := newHeaderAttributedClient(t)
	completions := client.Chat().Completions()
	proxy.names("", "alice@example.com")
	proxy.names("u-bob", "bob@example.com")
	anonymous := func() error {
		_, err := completions.New(context.Background(), chatRequest("openai/gpt-4.1-nano"))
		return err
	}

	assertReachesProxy(t, proxy, anonymous, "the first call, before the proxy has named anyone")
	assertBlocked(t, proxy, anonymous, "alice, named by the proxy")
	assertBlocked(t, proxy, func() error {
		_, err := completions.NewStreaming(context.Background(), chatRequest("openai/gpt-4.1-nano"))
		return err
	}, "alice streaming")
	assertBlocked(t, proxy, func() error {
		_, err := client.Embeddings().New(context.Background(), EmbeddingRequest{Model: "openai/text-embedding-3-small", Input: "hi"})
		return err
	}, "alice embedding")

	bob := func() error { _, err := completions.New(context.Background(), chatAs("u-bob")); return err }
	assertReachesProxy(t, proxy, bob, "bob's first call")
	assertReachesProxy(t, proxy, bob, "bob, under his cap")
	assertReachesProxy(t, proxy, func() error {
		_, err := completions.New(asSubscriber("bob@example.com"), chatRequest("openai/gpt-4.1-nano"))
		return err
	}, "a caller who names bob himself")
}

func TestCostControls_TheProxyPersonIsForgottenOnlyWhenASuccessfulAnswerNamesNobody(t *testing.T) {
	proxy, client := newHeaderAttributedClient(t)
	completions := client.Chat().Completions()
	anonymous := func() error {
		_, err := completions.New(context.Background(), chatRequest("openai/gpt-4.1-nano"))
		return err
	}
	remembered := func() interface{} { return client.proxyPeople.recall("")["email"] }

	proxy.names("", "bob@example.com")
	assertReachesProxy(t, proxy, anonymous, "bob")
	assert.Equal(t, "bob@example.com", remembered())

	proxy.names("", "")
	proxy.answers(http.StatusTooManyRequests)
	assertReachesProxy(t, proxy, anonymous, "bob, refused by the proxy without attribution")
	assert.Equal(t, "bob@example.com", remembered(), "an error answer without attribution keeps the person")

	proxy.answers(http.StatusOK)
	assertReachesProxy(t, proxy, anonymous, "a call the proxy attributes to nobody")
	assert.Nil(t, remembered())

	proxy.names("", "alice@example.com")
	assertReachesProxy(t, proxy, anonymous, "alice's first call")
	assertBlocked(t, proxy, anonymous, "alice")
}

func TestProxyPeople_ForgetsEveryoneRatherThanGrowWithoutBound(t *testing.T) {
	var people proxyPeople
	for i := 0; i < proxyPeopleLimit; i++ {
		people.remember(fmt.Sprintf("user-%d", i), map[string]interface{}{"email": "a@example.com"})
	}
	people.remember("one-more", map[string]interface{}{"email": "b@example.com"})

	assert.Len(t, people.byUser, 1)
	assert.Equal(t, "b@example.com", people.recall("one-more")["email"])
}

func TestProxyPeople_RefreshingAKnownUserAtTheLimitKeepsEveryone(t *testing.T) {
	var people proxyPeople
	for i := 0; i < proxyPeopleLimit; i++ {
		people.remember(fmt.Sprintf("user-%d", i), map[string]interface{}{"email": "a@example.com"})
	}
	people.remember("user-7", map[string]interface{}{"email": "c@example.com"})

	assert.Len(t, people.byUser, proxyPeopleLimit)
	assert.Equal(t, "c@example.com", people.recall("user-7")["email"])
	assert.Equal(t, "a@example.com", people.recall("user-0")["email"])
}
