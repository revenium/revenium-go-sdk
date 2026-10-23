package enforcement

import (
	"context"
	"errors"
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
	pollInterval = 30 * time.Second
	maxJitter    = 5 * time.Second
	fetchTimeout = 10 * time.Second
)

// poller keeps one team's rule set fresh in the background.
type poller struct {
	baseURL string
	apiKey  string
	teamID  string
	client  *http.Client
	cache   *cache

	mu      sync.Mutex
	stopped bool
	cancel  context.CancelFunc
	wg      sync.WaitGroup
}

func newPoller(key engineKey) *poller {
	return &poller{
		baseURL: key.baseURL,
		apiKey:  key.apiKey,
		teamID:  key.teamID,
		client:  core.NewReveniumHTTPClient(fetchTimeout),
		cache:   newCache(),
		cancel:  func() {},
	}
}

func (p *poller) boot() {
	if p.teamID == "" {
		core.Warn("enforcement: REVENIUM_TEAM_ID not set; enforcement rules will not be fetched. Set REVENIUM_TEAM_ID (the hashed team id) to enable client-side enforcement.")
		return
	}

	// Reject non-http(s) baseURLs so a misconfigured env var can't cause
	// the engine to send the API key to an attacker-controlled endpoint
	// (e.g. file://, ftp://, or a schemeless host).
	if !isValidBaseURL(p.baseURL) {
		core.Warn("enforcement: refusing to start — baseURL %q must be an absolute http(s) URL", p.baseURL)
		return
	}

	ctx, running := p.begin()
	if !running {
		return
	}

	if err := p.fetchRules(ctx); err != nil {
		reportFetchFailure("initial rule fetch failed, will retry in background", err)
	}

	go p.poll(ctx)

	core.Info("enforcement: engine started, polling %s%s%s", p.baseURL, core.EnforcementRulesPathPrefix, p.teamID)
}

// begin claims the poller's lifetime for the initial fetch and the poll
// loop. It refuses once stop has run, so a stop that races a boot never
// leaves a poll behind.
func (p *poller) begin() (context.Context, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.stopped {
		return nil, false
	}
	ctx, cancel := context.WithCancel(context.Background())
	p.cancel = cancel
	p.wg.Add(1)
	return ctx, true
}

func (p *poller) isStopped() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.stopped
}

func (p *poller) stop() {
	p.mu.Lock()
	p.stopped = true
	p.cancel()
	p.mu.Unlock()
	p.wg.Wait()
}

// poll runs the background loop that refreshes rules periodically.
func (p *poller) poll(ctx context.Context) {
	defer p.wg.Done()

	for {
		jitter := time.Duration(rand.Int63n(int64(maxJitter))) //nolint:gosec
		timer := time.NewTimer(pollInterval + jitter)

		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
			if err := p.fetchRules(ctx); err != nil {
				reportFetchFailure("rule fetch failed", err)
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
func (p *poller) fetchRules(ctx context.Context) error {
	if p.teamID == "" {
		return nil
	}

	url := p.baseURL + core.EnforcementRulesPathPrefix + p.teamID

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("x-api-key", p.apiKey)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Accept-Encoding", "gzip")
	req.Header.Set("User-Agent", "revenium-enforcement-go/1.0")

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("http get: %w", err)
	}
	defer resp.Body.Close()

	// 204 No Content = no rules configured for this team. Cache an empty
	// rule set so Check() returns Allowed and callers don't retry.
	if resp.StatusCode == http.StatusNoContent {
		p.cache.update(ruleSet{})
		core.Debug("enforcement: server returned 204, no rules configured")
		return nil
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return refusedFetchError(resp.StatusCode, body)
	}

	set, err := readRuleSet(resp, maxRulesAnswerBytes)
	if err != nil {
		return err
	}

	p.cache.update(set)
	core.Debug("enforcement: refreshed %d rules", len(set.rules))
	return nil
}

// reportFetchFailure raises an answer that arrived but could not be read or
// decoded to ERROR: unlike an outage it fails the same way on every poll, so
// a process that never loaded the rules enforces none of them and one that
// did loses all of them once they go stale.
func reportFetchFailure(stage string, err error) {
	if errors.Is(err, errUnreadableRules) {
		core.Error("enforcement: %s: %v; the last rules that loaded stay in force until they are %s old, and no rules apply if none have loaded yet", stage, err, ruleStaleAfter)
		return
	}
	core.Warn("enforcement: %s: %v", stage, err)
}
