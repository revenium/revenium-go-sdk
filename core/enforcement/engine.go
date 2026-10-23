package enforcement

import (
	"runtime"
	"sync"
	"sync/atomic"

	"github.com/revenium/revenium-go-sdk/core"
)

// Engine is one client's lease on the enforcement rules of a base URL, API
// key and team. Clients configured with the same three values share one
// background poll, which runs until the last of them calls Release; clients
// for different teams never see each other's rules. A lease follows Stop
// and Start: while stopped it allows every call, and once Start runs again
// for the same credentials it holds a lease on the new poll too, so it
// judges calls by the freshly loaded rules until it is itself released.
type Engine struct {
	key      engineKey
	lease    *lease
	defaults callDefaults
}

type engineKey struct {
	baseURL string
	apiKey  string
	teamID  string
}

// lease is the registry's record of one Engine's claim on a poll. It lives
// outside the Engine so the registry never keeps an abandoned Engine
// reachable: when a client is dropped without Close, the Engine's finalizer
// releases the lease and the poll can still drain.
type lease struct {
	key          engineKey
	registration *registration
	released     atomic.Bool
}

// registration is one running poller and the gate that later Start calls
// for the same credentials wait on until its initial fetch is done.
type registration struct {
	key     engineKey
	poller  *poller
	ready   chan struct{}
	leases  int
	started uint64
}

var (
	enginesMu     sync.RWMutex
	registrations = map[engineKey]*registration{}
	unreleased    = map[engineKey]map[*lease]struct{}{}
	registered    uint64
)

// Start takes a lease on the rules for these credentials, starting their
// background polling unless it is already running. Call Release on the
// returned Engine when the client is closed. The initial rule fetch runs
// outside the registry lock, so a slow team never delays another team's
// Start; a concurrent Start for the same credentials waits for that fetch.
// If teamID is empty, the engine is returned with an empty cache and no
// background polling — a single warning is logged so operators know why
// client-side enforcement is inactive for that client.
func Start(baseURL, apiKey, teamID string) *Engine {
	key := engineKey{baseURL: core.RulesAddress(baseURL), apiKey: apiKey, teamID: teamID}
	e, reg, owner := register(key)
	if owner {
		defer close(reg.ready)
		reg.poller.boot()
	} else {
		<-reg.ready
	}
	return e
}

func register(key engineKey) (*Engine, *registration, bool) {
	enginesMu.Lock()
	defer enginesMu.Unlock()

	reg, running := registrations[key]
	if !running {
		registered++
		reg = &registration{key: key, poller: newPoller(key), ready: make(chan struct{}), started: registered}
		registrations[key] = reg
		for held := range unreleased[key] {
			held.registration = reg
			reg.leases++
		}
	}
	reg.leases++
	l := &lease{key: key, registration: reg}
	if unreleased[key] == nil {
		unreleased[key] = map[*lease]struct{}{}
	}
	unreleased[key][l] = struct{}{}
	e := &Engine{key: key, lease: l}
	runtime.SetFinalizer(e, (*Engine).Release)
	return e, reg, !running
}

// Release ends this client's lease. When the last lease on a base URL, key
// and team is released, that team's poll stops and its rules are dropped;
// other teams are untouched. Releasing twice does nothing.
func (e *Engine) Release() {
	if e == nil {
		return
	}
	runtime.SetFinalizer(e, nil)
	e.lease.release()
}

func (l *lease) release() {
	if l == nil || !l.released.CompareAndSwap(false, true) {
		return
	}
	if reg := l.end(); reg != nil {
		reg.poller.stop()
	}
}

func (l *lease) end() *registration {
	enginesMu.Lock()
	defer enginesMu.Unlock()

	delete(unreleased[l.key], l)
	if len(unreleased[l.key]) == 0 {
		delete(unreleased, l.key)
	}
	reg, running := registrations[l.key]
	if !running || reg != l.registration {
		return nil
	}
	reg.leases--
	if reg.leases > 0 {
		return nil
	}
	delete(registrations, l.key)
	return reg
}

// StartFor takes a lease on the Engine for a client's Revenium
// configuration. Rules are fetched from EnforcementBaseURL with
// EnforcementAPIKey, so a read key can serve the rules while the metering key
// stays a metering key. Each setting is taken from the client's own config
// first (its enforcement value, then its metering value), and only then from
// REVENIUM_ENFORCEMENT_BASE_URL or REVENIUM_ENFORCEMENT_API_KEY, so one
// process serving several accounts never fetches one account's rules with
// another's key. An address still unset resolves the way the metering client
// resolves it. Initialize merges the environment into the config first, so
// its clients take the environment's enforcement settings at the first step.
func StartFor(rc *core.ReveniumConfig) *Engine {
	if rc == nil {
		return nil
	}
	env := core.LoadReveniumConfig()
	baseURL := firstSet(rc.RulesBaseURL(), env.EnforcementBaseURL, core.ResolveReveniumBaseURL(""))
	apiKey := firstSet(rc.RulesAPIKey(), env.EnforcementAPIKey)
	e := Start(baseURL, apiKey, rc.TeamID)
	e.defaults = callDefaultsFor(rc)
	return e
}

func firstSet(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

// Stop shuts down every background poll and drops the loaded rules, so
// every Engine allows all calls until Start runs again.
func Stop() {
	enginesMu.Lock()
	defer enginesMu.Unlock()

	if len(registrations) == 0 {
		return
	}
	for key, reg := range registrations {
		reg.poller.stop()
		delete(registrations, key)
	}
	core.Info("enforcement: engines stopped")
}

// Get returns an Engine for the earliest started poll still running, or
// nil if none is running. It holds no lease: releasing it does nothing.
//
// Deprecated: with several clients in one process the earliest poll may
// belong to another team. Keep the *Engine returned by Start and call its
// Check method instead.
func Get() *Engine {
	enginesMu.RLock()
	defer enginesMu.RUnlock()

	var earliest *registration
	for _, reg := range registrations {
		if earliest == nil || reg.started < earliest.started {
			earliest = reg
		}
	}
	if earliest == nil {
		return nil
	}
	return &Engine{key: earliest.key}
}

// Check evaluates the current rule set against the given context and
// returns ErrCostLimitExceeded when a BLOCK rule fires. A nil return
// means the request is allowed (including the case where the engine is
// stopped or dormant because no teamID or baseURL is configured —
// client-side enforcement fails open).
func (e *Engine) Check(ec EvalContext) error {
	p := e.poller()
	if p == nil {
		return nil
	}
	return evaluate(p.cache.snapshot(), e.defaults.complete(ec))
}

func (e *Engine) poller() *poller {
	if e == nil {
		return nil
	}
	enginesMu.RLock()
	defer enginesMu.RUnlock()
	if reg, ok := registrations[e.key]; ok {
		return reg.poller
	}
	return nil
}

// Check evaluates against the engine returned by Get. Returns nil when no
// engine is started — this is intentional (enforcement fails open).
//
// Deprecated: it judges every caller by the earliest running team. Call
// Check on the *Engine returned by Start instead.
func Check(ec EvalContext) error {
	return Get().Check(ec)
}
