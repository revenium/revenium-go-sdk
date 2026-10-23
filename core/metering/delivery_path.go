package metering

import (
	"sync"

	"github.com/revenium/revenium-go-sdk/core/resilience"
)

// deliveryPath is what live sends and resends of one client share: how a
// request is sent, the key's breaker, the process-wide send slots and the
// delivery monitor. The client and its buffer point at the same value, so
// they cannot drift apart.
type deliveryPath struct {
	sender    *recordSender
	breaker   *resilience.CircuitBreaker
	sendLimit *resilience.ConcurrencyLimit
	monitor   *deliveryMonitor
}

func newDeliveryPath(breaker *resilience.CircuitBreaker, sender *recordSender) *deliveryPath {
	return &deliveryPath{
		sender:    sender,
		breaker:   breaker,
		sendLimit: sharedSendSlots,
		monitor:   &deliveryMonitor{},
	}
}

func (p *deliveryPath) sendOnce(post func() error) error {
	return p.sendLimit.Run(func() error { return p.breaker.ExecuteHonoringRetryAfter(post) })
}

func (p *deliveryPath) sendWithRetries(retryConfig *resilience.RetryConfig, post func() error) error {
	return resilience.RetryThroughBreaker(p.breaker, p.sendLimit, retryConfig, post)
}

type activity struct {
	mu    sync.Mutex
	count int
	idle  chan struct{}
}

func (a *activity) begin() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.count == 0 {
		a.idle = make(chan struct{})
	}
	a.count++
}

func (a *activity) end() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.count--
	if a.count == 0 {
		close(a.idle)
	}
}

func (a *activity) running() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.count
}

func (a *activity) finished() <-chan struct{} {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.count == 0 {
		return closedChannel
	}
	return a.idle
}

var closedChannel = func() chan struct{} {
	ch := make(chan struct{})
	close(ch)
	return ch
}()
