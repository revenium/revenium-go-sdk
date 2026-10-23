package metering

import (
	"sync"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/revenium/revenium-go-sdk/core/resilience"
)

// One pool per process runs background sends, a second runs resends of held
// records, and one set of slots caps every metering request in flight (live
// sends, resends and SendSync callers), so a slow Revenium on any number of
// hosts cannot make the SDK open a socket per call and starve the
// application of file descriptors. The numbers match the Python SDK's
// metering pool and buffer: 32 senders with room for 1,000 waiting records
// (records beyond that are held in the client's buffer and resent later),
// and 16 resends at a time.
const (
	deliveryWorkers   = core.ReveniumMaxConnsPerHost
	deliveryQueueSize = 1000
)

var (
	sharedDeliveryPool = newDeliveryPool(deliveryWorkers, deliveryQueueSize)
	sharedReplayPool   = newDeliveryPool(replayConcurrency, 0)
	sharedSendSlots    = resilience.NewConcurrencyLimit(core.ReveniumMaxConnsPerHost)
)

type deliveryPool struct {
	workers int
	jobs    chan func()
	start   sync.Once
}

func newDeliveryPool(workers, queueSize int) *deliveryPool {
	return &deliveryPool{workers: workers, jobs: make(chan func(), queueSize)}
}

func (p *deliveryPool) trySubmit(job func()) bool {
	p.start.Do(p.startWorkers)
	select {
	case p.jobs <- job:
		return true
	default:
		return false
	}
}

func (p *deliveryPool) submit(job func()) {
	p.start.Do(p.startWorkers)
	p.jobs <- job
}

func (p *deliveryPool) hasRoom() bool {
	return len(p.jobs) <= cap(p.jobs)/2
}

func (p *deliveryPool) startWorkers() {
	for i := 0; i < p.workers; i++ {
		go p.work()
	}
}

func (p *deliveryPool) work() {
	for job := range p.jobs {
		runJob(job)
	}
}

func runJob(job func()) {
	defer func() {
		if r := recover(); r != nil {
			core.Error("[METERING] panic in delivery worker: %v", r)
		}
	}()
	job()
}
