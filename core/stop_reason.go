package core

// ReveniumStopReason represents the standardized stop reasons for Revenium metering API
type ReveniumStopReason string

const (
	StopReasonEnd             ReveniumStopReason = "END"
	StopReasonEndSequence     ReveniumStopReason = "END_SEQUENCE"
	StopReasonTimeout         ReveniumStopReason = "TIMEOUT"
	StopReasonTokenLimit      ReveniumStopReason = "TOKEN_LIMIT"
	StopReasonCostLimit       ReveniumStopReason = "COST_LIMIT"
	StopReasonCompletionLimit ReveniumStopReason = "COMPLETION_LIMIT"
	StopReasonError           ReveniumStopReason = "ERROR"
	StopReasonCancelled       ReveniumStopReason = "CANCELLED"
)
