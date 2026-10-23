package litellm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/revenium/revenium-go-sdk/core"
)

const (
	modelInfoPath      = "/model/info"
	modelInfoUserAgent = "revenium-go-sdk/1.0"
	modelInfoTimeout   = 5 * time.Second
)

// modelRoutesTTL is long because a proxy's model list changes only when its operator edits the config,
// so one read covers many calls.
const modelRoutesTTL = 5 * time.Minute

// modelRoutesRetryAfter is short so a passing proxy outage does not leave aliases judged by an old list for long.
const modelRoutesRetryAfter = 30 * time.Second

// modelRoutes maps each model name a caller can request to the provider models the proxy serves it with,
// and each deployment id the proxy names in its answers to the provider model that deployment serves.
type modelRoutes struct {
	mu            sync.Mutex
	routes        map[string][]string
	deployments   map[string]string
	expiresAt     time.Time
	failureLogged bool
	refreshing    chan struct{}
}

type modelInfoAnswer struct {
	Data []struct {
		ModelName     string `json:"model_name"`
		LiteLLMParams struct {
			Model string `json:"model"`
		} `json:"litellm_params"`
		ModelInfo struct {
			ID string `json:"id"`
		} `json:"model_info"`
	} `json:"data"`
}

type proxyModelList struct {
	routes      map[string][]string
	deployments map[string]string
}

func (a modelInfoAnswer) modelList() proxyModelList {
	list := proxyModelList{routes: map[string][]string{}, deployments: map[string]string{}}
	for _, deployment := range a.Data {
		served := strings.TrimSpace(deployment.LiteLLMParams.Model)
		if served == "" {
			continue
		}
		if id := strings.TrimSpace(deployment.ModelInfo.ID); id != "" {
			list.deployments[id] = served
		}
		if deployment.ModelName != "" {
			list.routes[deployment.ModelName] = appendUnique(list.routes[deployment.ModelName], served)
		}
	}
	return list
}

// LiteLLM answers with the model name the caller asked for, so an alias such as team-fast
// comes back as team-fast; these response headers name the deployment that actually served it.
const (
	servedDeploymentHeader   = "X-Litellm-Model-Name"
	servedDeploymentIDHeader = "X-Litellm-Model-Id"
	servedModelGroupHeader   = "X-Litellm-Model-Group"
)

// servedDeployment names the provider model the proxy served the call with: the deployment its headers
// name, else the deployment its id or model group resolves to in the model list last read for cost
// controls, else "". The cost-control check judges the same deployments, so a cap counts the spend it blocks.
func (r *ReveniumLiteLLM) servedDeployment(headers http.Header) string {
	if deployment := filledHeader(headers, servedDeploymentHeader); deployment != "" {
		return deployment
	}
	return r.modelRoutes.deployment(filledHeader(headers, servedDeploymentIDHeader), filledHeader(headers, servedModelGroupHeader))
}

// recordedModel is the model a chat call is recorded under. An answer model that only echoes the requested
// name gives way to the served deployment; one the proxy passed through from the provider, such as a dated
// snapshot, is the most precise name there is and is kept.
func recordedModel(answerModel, requestedModel, deployment string, headers http.Header) string {
	echoesRequest := answerModel == requestedModel || answerModel == filledHeader(headers, servedModelGroupHeader)
	switch {
	case answerModel != "" && !echoesRequest:
		return answerModel
	case deployment != "":
		return deployment
	case answerModel != "":
		return answerModel
	}
	return requestedModel
}

// deployment reads only the list already cached, so naming a record never waits on the proxy;
// a model group routed to several deployments names none, since the one served is unknown.
func (m *modelRoutes) deployment(id, group string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if served := m.deployments[id]; id != "" && served != "" {
		return served
	}
	if served := m.routes[group]; group != "" && len(served) == 1 {
		return served[0]
	}
	return ""
}

// costControlModels lists every model a chat call can be recorded under: the requested name, and
// each provider model the proxy routes that name to. The record names the model the proxy served,
// which for an alias such as team-fast is only known from the proxy's own model list before the call.
func (r *ReveniumLiteLLM) costControlModels(ctx context.Context, requestedModel string) []string {
	models := []string{requestedModel}
	if !r.enforcesCostControls() {
		return models
	}
	for _, served := range r.modelRoutes.lookup(ctx, r, requestedModel) {
		models = appendUnique(models, served)
	}
	return models
}

func (r *ReveniumLiteLLM) enforcesCostControls() bool {
	return r.config.Revenium != nil && r.config.Revenium.TeamID != ""
}

// lookup waits for an expired list to be read again only as long as the caller's context allows; a caller
// that stops waiting is judged by the last list read, and the shared read carries on for the next caller.
func (m *modelRoutes) lookup(ctx context.Context, r *ReveniumLiteLLM, requestedModel string) []string {
	if refreshed := m.refreshIfExpired(ctx, r); refreshed != nil {
		select {
		case <-refreshed:
		case <-ctx.Done():
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.routes[requestedModel]
}

// refreshIfExpired starts at most one read of the proxy's model list at a time and returns the channel
// closed when that read ends, or nil while the list read last is still fresh.
func (m *modelRoutes) refreshIfExpired(ctx context.Context, r *ReveniumLiteLLM) <-chan struct{} {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.refreshing == nil && time.Now().After(m.expiresAt) {
		m.refreshing = make(chan struct{})
		go m.refresh(ctx, r, m.refreshing)
	}
	return m.refreshing
}

func (m *modelRoutes) refresh(ctx context.Context, r *ReveniumLiteLLM, done chan struct{}) {
	list, err := r.fetchModelRoutes(ctx)
	m.mu.Lock()
	defer m.mu.Unlock()
	defer close(done)
	m.refreshing = nil
	m.record(list, err)
}

func (m *modelRoutes) record(list proxyModelList, err error) {
	if err != nil {
		if !m.failureLogged {
			core.Warn("LiteLLM: could not read the proxy's model list, so cost controls keep the last list read; aliases not in it are judged by the requested name: %v", err)
			m.failureLogged = true
		}
		m.expiresAt = time.Now().Add(modelRoutesRetryAfter)
		return
	}
	m.routes, m.deployments, m.failureLogged = list.routes, list.deployments, false
	m.expiresAt = time.Now().Add(modelRoutesTTL)
}

// fetchModelRoutes reads the proxy's model list on a context detached from the caller's cancellation,
// since the answer is shared: a caller that gives up must not be cached as a proxy that failed.
func (r *ReveniumLiteLLM) fetchModelRoutes(ctx context.Context) (proxyModelList, error) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), modelInfoTimeout)
	defer cancel()

	url := strings.TrimRight(r.config.LiteLLMProxyURL, "/") + modelInfoPath
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return proxyModelList{}, core.NewNetworkError("failed to create the model list request", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", modelInfoUserAgent)
	if r.config.LiteLLMAPIKey != "" {
		req.Header.Set("Authorization", "Bearer "+r.config.LiteLLMAPIKey)
	}
	resp, err := r.httpClient.Do(req)
	if err != nil {
		return proxyModelList{}, core.NewNetworkError("model list request to LiteLLM proxy failed", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, resp.Body)
		return proxyModelList{}, core.NewProviderError(fmt.Sprintf("LiteLLM proxy answered %s to %s", resp.Status, modelInfoPath), nil)
	}
	var answer modelInfoAnswer
	if err := json.NewDecoder(resp.Body).Decode(&answer); err != nil {
		return proxyModelList{}, core.NewProviderError("failed to decode the LiteLLM proxy's model list", err)
	}
	return answer.modelList(), nil
}

func appendUnique(list []string, value string) []string {
	for _, existing := range list {
		if existing == value {
			return list
		}
	}
	return append(list, value)
}
