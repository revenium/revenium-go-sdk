package enforcement

import "fmt"

// ErrCostLimitExceeded is returned by Engine.Check when a request is blocked
// by an enforcement rule. The struct mirrors the Node SDK's CostLimitExceeded
// class so customers moving between SDKs see the same field surface. Callers
// recover the structured fields with errors.As:
//
//	var ece *enforcement.ErrCostLimitExceeded
//	if errors.As(err, &ece) {
//	    // use ece.Threshold, ece.CurrentValue, etc.
//	}
type ErrCostLimitExceeded struct {
	// RuleID is the server-side rule identifier. Stringified from the numeric
	// ruleId in the API response for consistency with the Node SDK contract.
	RuleID string
	// RuleName is the human-readable rule name when the server provides one.
	RuleName string
	// Threshold is the rule's configured limit.
	Threshold float64
	// CurrentValue is the subscriber's metered value at the moment of the block.
	CurrentValue float64
	// PeriodType is the rule's billing window (DAILY / WEEKLY / MONTHLY /
	// QUARTERLY). Empty when the server omits it.
	PeriodType string
	// Action is the rule action that triggered the block (always BLOCK today,
	// but retained for forward compatibility with future hard-stop actions).
	Action Action
	// Context carries the evaluation criteria that matched the rule —
	// subscriberId, productName, model, provider. Surfaced so callers can log
	// or surface which call was rejected.
	Context EvalContext
}

// Error implements the error interface.
func (e *ErrCostLimitExceeded) Error() string {
	period := e.PeriodType
	if period == "" {
		period = "limit"
	}
	return fmt.Sprintf("cost limit exceeded: $%.2f of $%.2f %s reached (rule %s)",
		e.CurrentValue, e.Threshold, period, e.RuleID)
}

// Is lets callers match with errors.Is against a package-level sentinel if
// they don't need the structured fields.
func (e *ErrCostLimitExceeded) Is(target error) bool {
	_, ok := target.(*ErrCostLimitExceeded)
	return ok
}
