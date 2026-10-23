package resilience

import "errors"

var ErrCircuitOpen = errors.New("circuit breaker is open")

type circuitOpenError struct {
	cause error
}

func (e *circuitOpenError) Error() string {
	return ErrCircuitOpen.Error() + " after: " + e.cause.Error()
}

func (e *circuitOpenError) Unwrap() []error {
	return []error{ErrCircuitOpen, e.cause}
}
