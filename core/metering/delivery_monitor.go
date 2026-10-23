package metering

import (
	"sync/atomic"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/revenium/revenium-go-sdk/core/resilience"
)

type deliveryMonitor struct {
	failing    atomic.Bool
	onRecovery func()
}

func (m *deliveryMonitor) record(err error) {
	if m == nil {
		return
	}
	switch {
	case err == resilience.ErrCircuitOpen:
		return
	case err == nil:
		if m.failing.CompareAndSwap(true, false) {
			core.Info("[METERING] usage records are reaching Revenium again")
			if m.onRecovery != nil {
				m.onRecovery()
			}
		}
	case isBufferable(err):
		if m.failing.CompareAndSwap(false, true) {
			core.Warn("[METERING] usage records are not reaching Revenium: %v. They are kept in memory and retried, and any still pending when the process exits are lost. Check network access to the Revenium API, including HTTP_PROXY, HTTPS_PROXY and NO_PROXY.", err)
		}
	}
}
