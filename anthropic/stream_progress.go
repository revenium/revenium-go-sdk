package anthropic

// The stop event is message_stop, or messageStop on Bedrock Converse.
type streamProgress struct {
	stopEventSeen bool
	exhausted     bool
}

func (p streamProgress) abandonedBeforeStop() bool {
	return !p.stopEventSeen && !p.exhausted
}

type stopEventReporter interface {
	reachedStopEvent() bool
}
