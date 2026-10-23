package litellm

import (
	"context"
	"net/http"
	"sync"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/revenium/revenium-go-sdk/core/enforcement"
)

// When a new `user` value would take a client past this many, it forgets them
// all and relearns each from the proxy's next answer, so memory stays bounded;
// refreshing a `user` it already knows never forgets anyone.
const proxyPeopleLimit = 1024

// The proxy names the person only in its response headers, after the call, so
// a call is checked as the person the proxy named for the same user last time
// and the very first call of a header-only person cannot be checked.
type proxyPeople struct {
	mu     sync.Mutex
	byUser map[string]map[string]interface{}
}

func (p *proxyPeople) remember(user string, subscriber map[string]interface{}) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(subscriber) == 0 {
		delete(p.byUser, user)
		return
	}
	if p.byUser == nil || (!p.knows(user) && len(p.byUser) >= proxyPeopleLimit) {
		p.byUser = make(map[string]map[string]interface{})
	}
	p.byUser[user] = subscriber
}

func (p *proxyPeople) knows(user string) bool {
	_, known := p.byUser[user]
	return known
}

func (p *proxyPeople) recall(user string) map[string]interface{} {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.byUser[user]
}

func requestUser(payload interface{}) string {
	switch request := payload.(type) {
	case ChatCompletionRequest:
		return request.User
	case EmbeddingRequest:
		return request.User
	default:
		return ""
	}
}

// An error answer without attribution headers says nothing about who the
// caller is, so only a successful answer that names nobody forgets the person.
func (r *ReveniumLiteLLM) rememberProxyPerson(payload interface{}, answer *http.Response) {
	subscriber := subscriberFromHeaders(answer.Header)
	if len(subscriber) == 0 && (answer.StatusCode < 200 || answer.StatusCode >= 300) {
		return
	}
	r.proxyPeople.remember(requestUser(payload), subscriber)
}

// checkCostControls departs from the usual enforcer.Check(NewEvalContext(...))
// call because NewEvalContext reads only the context's metadata, while the
// person here may come from the proxy's earlier answer; merging it under the
// caller's metadata judges the call as its usage record will attribute it.
func (r *ReveniumLiteLLM) checkCostControls(ctx context.Context, requestedModel string, payload interface{}) error {
	return r.checkCostControlsForModels(ctx, r.costControlModels(ctx, requestedModel), payload, true)
}

// checkEmbeddingCostControls judges an embedding by its requested model only:
// its record is filed under that model, so a route the proxy may take must
// not block what the platform would never count.
func (r *ReveniumLiteLLM) checkEmbeddingCostControls(ctx context.Context, requestedModel string, payload interface{}) error {
	return r.checkCostControlsForModels(ctx, []string{requestedModel}, payload, false)
}

// checkCostControlsForModels judges the call as every one of models, as the
// person the proxy remembered; recordsServedName says the record will carry the
// name the proxy served (chat), so dated-name matching may apply, while an
// embedding records the requested name and compares exactly.
func (r *ReveniumLiteLLM) checkCostControlsForModels(ctx context.Context, models []string, payload interface{}, recordsServedName bool) error {
	metadata := core.GetUsageMetadata(ctx)
	if subscriber := r.proxyPeople.recall(requestUser(payload)); subscriber != nil {
		metadata = MergeMetadata(metadata, map[string]interface{}{"subscriber": subscriber})
	}
	for _, model := range models {
		evalCtx := enforcement.NewEvalContextFromMetadata(ctx, metadata, ExtractModelName(model), ExtractProvider(model))
		if recordsServedName && servesDatedSnapshots(model) {
			evalCtx = evalCtx.ServedModelMayBeDated()
		}
		if err := r.enforcer.Check(evalCtx); err != nil {
			return err
		}
	}
	return nil
}

// servesDatedSnapshots says whether the provider a LiteLLM route points at can
// serve a model under a dated snapshot, so a MODEL filter follows the dated-name
// rules. A name no provider pattern recognises (a bare OpenAI reasoning model
// such as o3, or an alias the proxy's model list did not resolve) is treated as
// one that may be dated, because the proxy records the name it served; only a
// route to a provider known to serve exactly the requested name compares exactly.
func servesDatedSnapshots(model string) bool {
	switch ExtractProvider(model) {
	case "OpenAI", "Azure", "Anthropic", "XAI", "LiteLLM":
		return true
	}
	return false
}
