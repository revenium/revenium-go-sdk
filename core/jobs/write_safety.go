package jobs

import (
	"errors"
	"net"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/revenium/revenium-go-sdk/core/resilience"
)

// refusedBeforeApplying counts only an HTTP answer the shared classifier
// calls throttled: Revenium refused the request without applying it. A
// throttling-like message with no HTTP status proves nothing about that.
func refusedBeforeApplying(err error) bool {
	var revErr *core.ReveniumError
	return errors.As(err, &revErr) && revErr.StatusCode != 0 &&
		resilience.ClassifyHTTPResponse(revErr.StatusCode, revErr.Message) == resilience.ClassificationThrottled
}

func anyMayHaveBeenApplied(attempts []error) bool {
	for _, err := range attempts {
		if mayHaveBeenApplied(err) {
			return true
		}
	}
	return false
}

func mayHaveBeenApplied(err error) bool {
	return err != nil && !refusedBeforeApplying(err)
}

func timedOutWaitingForRevenium(err error) bool {
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

// timeoutResendPolicy allows one resend after a timeout. The shared
// classifier refuses an http.Client timeout but retries a timeout raised by
// the connection itself, so the policy both widens and caps timeout retries
// from its own count of the call's timeouts.
type timeoutResendPolicy struct {
	timeouts int
}

func (p *timeoutResendPolicy) record(attempt error) {
	if timedOutWaitingForRevenium(attempt) {
		p.timeouts++
	}
}

func (p *timeoutResendPolicy) allowsRetry(err error) bool {
	return !timedOutWaitingForRevenium(err) || p.timeouts <= 1
}

func (p *timeoutResendPolicy) widensRetry(err error) bool {
	return timedOutWaitingForRevenium(err) && p.timeouts <= 1
}
