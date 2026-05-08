package enforcement

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/revenium/revenium-go-sdk/core"
)

const (
	enforcementPathPrefix = "/v2/api/ai/enforcement-rules/"
	pollInterval          = 30 * time.Second
	maxJitter             = 5 * time.Second
	fetchTimeout          = 10 * time.Second
)

// Engine is the singleton enforcement engine that polls the Revenium API for
// enforcement rules and evaluates them against incoming requests.
type Engine struct {
	baseURL string
	apiKey  string
	teamID  string
	client  *http.Client
	cache   *cache
	cancel  context.CancelFunc
	wg      sync.WaitGroup
}

var (
	instance   *Engine
	instanceMu sync.RWMutex
)

// Start initialises the singleton Engine and begins background polling.
// Calling Start more than once is safe; subsequent calls are no-ops.
// If teamID is empty, the engine is returned with an empty cache and no
// background polling — a single warning is logged so operators know why
// client-side enforcement is inactive.
func Start(baseURL, apiKey, teamID string) *Engine {
	instanceMu.Lock()
	defer instanceMu.Unlock()

	if instance != nil {
		if instance.baseURL != baseURL || instance.apiKey != apiKey || instance.teamID != teamID {
			core.Warn("enforcement: Start called with different credentials; using existing engine (%s)", instance.baseURL)
		}
		return instance
	}

	ctx, cancel := context.WithCancel(context.Background())

	e := &Engine{
		baseURL: baseURL,
		apiKey:  apiKey,
		teamID:  teamID,
		client: &http.Client{
			Timeout: fetchTimeout,
			Transport: &http.Transport{
				MaxIdleConns:        10,
				MaxIdleConnsPerHost: 5,
				IdleConnTimeout:     90 * time.Second,
			},
		},
		cache:  newCache(),
		cancel: cancel,
	}

	if teamID == "" {
		core.Warn("enforcement: REVENIUM_TEAM_ID not set; enforcement rules will not be fetched. Set REVENIUM_TEAM_ID (the hashed team id) to enable client-side enforcement.")
		instance = e
		return e
	}

	// Reject non-http(s) baseURLs so a misconfigured env var can't cause
	// the engine to send the API key to an attacker-controlled endpoint
	// (e.g. file://, ftp://, or a schemeless host).
	if !isValidBaseURL(baseURL) {
		core.Warn("enforcement: refusing to start — baseURL %q must be an absolute http(s) URL", baseURL)
		instance = e
		return e
	}

	if err := e.fetchRules(ctx); err != nil {
		core.Warn("enforcement: initial rule fetch failed, will retry in background: %v", err)
	}

	e.wg.Add(1)
	go e.poll(ctx)

	instance = e
	core.Info("enforcement: engine started, polling %s%s%s", baseURL, enforcementPathPrefix, teamID)
	return e
}

// Stop shuts down the background poller and clears the singleton.
func Stop() {
	instanceMu.Lock()
	defer instanceMu.Unlock()

	if instance == nil {
		return
	}
	instance.cancel()
	instance.wg.Wait()
	instance = nil
	core.Info("enforcement: engine stopped")
}

// Get returns the singleton engine, or nil if not started.
func Get() *Engine {
	instanceMu.RLock()
	defer instanceMu.RUnlock()
	return instance
}

// Check evaluates the current rule set against the given context and
// returns ErrCostLimitExceeded when a BLOCK rule fires. A nil return
// means the request is allowed (including the case where the engine is
// dormant because no teamID or baseURL is configured — client-side
// enforcement fails open).
func (e *Engine) Check(ec EvalContext) error {
	if e == nil {
		return nil
	}
	rules := e.cache.snapshot()
	return evaluate(rules, ec)
}

// Check is a package-level convenience that evaluates against the
// singleton engine. Returns nil when the engine is not started — this
// is intentional (enforcement fails open).
func Check(ec EvalContext) error {
	return Get().Check(ec)
}

// poll runs the background loop that refreshes rules periodically.
func (e *Engine) poll(ctx context.Context) {
	defer e.wg.Done()

	for {
		jitter := time.Duration(rand.Int63n(int64(maxJitter))) //nolint:gosec
		timer := time.NewTimer(pollInterval + jitter)

		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
			if err := e.fetchRules(ctx); err != nil {
				core.Warn("enforcement: rule fetch failed: %v", err)
			}
		}
	}
}

// isValidBaseURL returns true when baseURL is an absolute http(s) URL
// with a host. Used to gate enforcement startup so the API key is never
// shipped to an unexpected scheme or a schemeless string.
func isValidBaseURL(baseURL string) bool {
	u, err := url.Parse(baseURL)
	if err != nil {
		return false
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return false
	}
	return u.Host != ""
}

// fetchRules retrieves enforcement rules from the API and updates the cache.
func (e *Engine) fetchRules(ctx context.Context) error {
	if e.teamID == "" {
		return nil
	}

	url := e.baseURL + enforcementPathPrefix + e.teamID

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("x-api-key", e.apiKey)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "revenium-enforcement-go/1.0")

	resp, err := e.client.Do(req)
	if err != nil {
		return fmt.Errorf("http get: %w", err)
	}
	defer resp.Body.Close()

	// 204 No Content = no rules configured for this team. Cache an empty
	// rule set so Check() returns Allowed and callers don't retry.
	if resp.StatusCode == http.StatusNoContent {
		e.cache.update(nil)
		core.Debug("enforcement: server returned 204, no rules configured")
		return nil
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(body))
	}

	// The enforcement API returns a `{rules: [...]}` envelope. Decode into
	// the envelope first; fall back to a raw array for forward compatibility
	// with servers that emit either shape.
	var envelope struct {
		Rules []Rule `json:"rules"`
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("read body: %w", err)
	}
	if err := json.Unmarshal(body, &envelope); err != nil || envelope.Rules == nil {
		var rules []Rule
		if err2 := json.Unmarshal(body, &rules); err2 != nil {
			return fmt.Errorf("decode rules: envelope=%v array=%v", err, err2)
		}
		envelope.Rules = rules
	}

	e.cache.update(envelope.Rules)
	core.Debug("enforcement: refreshed %d rules", len(envelope.Rules))
	return nil
}
