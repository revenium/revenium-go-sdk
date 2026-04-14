package resilience

import "sync"

var (
	globalCB *CircuitBreaker
	globalMu sync.Mutex
)

func GetGlobalCircuitBreaker() *CircuitBreaker {
	globalMu.Lock()
	defer globalMu.Unlock()
	if globalCB == nil {
		strategy := &DefaultFailureStrategy{}
		globalCB = NewCircuitBreaker(CircuitBreakerConfig{
			ShouldTrip: strategy.IsRetryableError,
		})
	}
	return globalCB
}

func ResetGlobalCircuitBreaker() {
	globalMu.Lock()
	defer globalMu.Unlock()
	globalCB = nil
}
