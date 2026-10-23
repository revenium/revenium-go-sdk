package resilience

import (
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/revenium/revenium-go-sdk/core"
)

// The delivery breaker matches the Python SDK's delivery circuit: three
// failed sends in a row open it, one send is let through every five seconds
// while it is open, and the first success closes it again.
const (
	deliveryFailuresToOpen = 3
	deliveryProbeInterval  = 5 * time.Second
	deliverySuccessToClose = 1
)

var (
	globalCB      *CircuitBreaker
	scopedCBs     = map[string]*scopedBreaker{}
	globalMu      sync.Mutex
	defaultPolicy = &DefaultFailureStrategy{}
)

// GetGlobalCircuitBreaker returns the legacy process-wide breaker. No SDK
// call consults it: metering and job calls each hold a breaker per Revenium
// endpoint, base URL and key, so its state does not reflect delivery.
//
// Deprecated: use AcquireCircuitBreaker for the endpoint, base URL and key
// you call, and ResetCircuitBreakers to clear breaker state in tests.
func GetGlobalCircuitBreaker() *CircuitBreaker {
	globalMu.Lock()
	defer globalMu.Unlock()
	if globalCB == nil {
		globalCB = NewCircuitBreaker(CircuitBreakerConfig{
			ShouldTrip: defaultPolicy.IsRetryableError,
		})
	}
	return globalCB
}

func DeliveryCircuitBreakerConfig() CircuitBreakerConfig {
	return CircuitBreakerConfig{
		FailureThreshold:    deliveryFailuresToOpen,
		RecoveryTimeout:     deliveryProbeInterval,
		SuccessThreshold:    deliverySuccessToClose,
		ConsecutiveFailures: true,
		ShouldTrip:          defaultPolicy.IsRetryableError,
	}
}

// Endpoint names which Revenium API a breaker guards. Usage records and job
// calls get separate breakers for the same key, so a problem limited to the
// metering endpoint does not make job outcome reports fail fast.
type Endpoint string

const (
	MeteringEndpoint Endpoint = "metering"
	JobsEndpoint     Endpoint = "jobs"
)

type scopedBreaker struct {
	breaker *CircuitBreaker
	users   int
}

// AcquireCircuitBreaker returns the breaker shared by every client that calls
// this Revenium endpoint at this base URL with this key, and the function
// that gives it back.
// Revenium throttles per key, so one key's 429s or outage must not hold back
// another key's records; the breaker is forgotten once its last user is done.
func AcquireCircuitBreaker(endpoint Endpoint, baseURL, apiKey string) (*CircuitBreaker, func()) {
	scope := credentialScope(endpoint, baseURL, apiKey)
	globalMu.Lock()
	defer globalMu.Unlock()
	entry, ok := scopedCBs[scope]
	if !ok {
		entry = &scopedBreaker{breaker: NewCircuitBreaker(DeliveryCircuitBreakerConfig())}
		scopedCBs[scope] = entry
	}
	entry.users++
	var once sync.Once
	return entry.breaker, func() { once.Do(func() { releaseScopedBreaker(scope, entry) }) }
}

func releaseScopedBreaker(scope string, entry *scopedBreaker) {
	globalMu.Lock()
	defer globalMu.Unlock()
	entry.users--
	if entry.users == 0 && scopedCBs[scope] == entry {
		delete(scopedCBs, scope)
	}
}

func scopedBreakerCount() int {
	globalMu.Lock()
	defer globalMu.Unlock()
	return len(scopedCBs)
}

func credentialScope(endpoint Endpoint, baseURL, apiKey string) string {
	sum := sha256.Sum256([]byte(string(endpoint) + "\n" + scopeBaseURL(baseURL) + "\n" + strings.TrimSpace(apiKey)))
	return hex.EncodeToString(sum[:])
}

func scopeBaseURL(baseURL string) string {
	normalized := core.NormalizeReveniumBaseURL(strings.TrimSpace(baseURL))
	parsed, err := url.Parse(normalized)
	if err != nil || parsed.Host == "" {
		return strings.ToLower(normalized)
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	parsed.Host = strings.ToLower(parsed.Host)
	return parsed.String()
}

// ResetCircuitBreakers forgets the global breaker and every per-key breaker,
// so the next client for a key starts with a fresh one. Clients created
// before the reset keep the breaker they already hold. For tests.
func ResetCircuitBreakers() {
	globalMu.Lock()
	defer globalMu.Unlock()
	globalCB = nil
	scopedCBs = map[string]*scopedBreaker{}
}

// ResetGlobalCircuitBreaker forgets every breaker, like ResetCircuitBreakers.
//
// Deprecated: use ResetCircuitBreakers, which names what it does.
func ResetGlobalCircuitBreaker() {
	ResetCircuitBreakers()
}
