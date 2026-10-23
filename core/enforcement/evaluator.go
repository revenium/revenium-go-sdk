package enforcement

import (
	"strconv"

	"github.com/revenium/revenium-go-sdk/core"
)

const departmentBudgetFallbackName = "Department budget"

// evaluate applies a rule set to the request and returns
// ErrCostLimitExceeded when a BLOCK rule fires for this caller. It follows
// the platform's own order: the server's department verdict first, then
// every other rule whose filters cover the request, each judged on the
// caller's own balance when the rule is a per-person cap. Rules configured
// as observation-only (shadowMode / WARN_ONLY / THROTTLE) only log. A nil
// return means "allow".
//
// Callers should propagate the error without wrapping so consumers can use
// errors.As to recover the structured ErrCostLimitExceeded fields.
func evaluate(rs ruleSet, ec EvalContext) error {
	if err := rs.departmentBlock(ec); err != nil {
		return err
	}
	for i := range rs.rules {
		r := &rs.rules[i]
		if r.isDepartmentRule() || !r.covers(ec) {
			continue
		}
		currentValue, breached := r.breachFor(ec)
		if !breached {
			continue
		}
		if r.isObservationOnly() {
			core.Warn("enforcement: cost limit rule breached (observation only) — rule=%d name=%q action=%s shadowMode=%v threshold=%.4f currentValue=%.4f",
				r.RuleID, r.Name, r.Action, r.ShadowMode, r.Threshold, currentValue)
			continue
		}
		if r.normalizedAction() == ActionBlock {
			return r.costLimitError(currentValue, ec)
		}
		core.Warn("enforcement: cost limit rule breached but its action is not recognized, leaving enforcement to the platform — rule=%d name=%q action=%q threshold=%.4f currentValue=%.4f",
			r.RuleID, r.Name, r.Action, r.Threshold, currentValue)
	}
	return nil
}

// breachFor reports whether the rule is breached for this caller and the
// balance it was judged on. A per-person cap is judged only on the
// caller's own breakdown entry; when the payload carries no breakdown for
// a cost cap the SDK cannot tell whose balance is over, so it leaves the
// decision to the platform rather than block everyone.
func (r *Rule) breachFor(ec EvalContext) (float64, bool) {
	if !r.groupedBy(DimensionSubscriber) {
		return r.CurrentValue, r.Breached
	}
	if len(r.GroupBreakdown) > 0 {
		entry := r.groupEntryFor(ec)
		if entry == nil || !entry.Breached {
			return 0, false
		}
		return entry.CurrentValue, true
	}
	if r.measuresCost() {
		return 0, false
	}
	return r.CurrentValue, r.Breached
}

func (r *Rule) groupEntryFor(ec EvalContext) *GroupEntry {
	for _, key := range ec.groupKeys() {
		for i := range r.GroupBreakdown {
			if r.GroupBreakdown[i].GroupValue == key {
				return &r.GroupBreakdown[i]
			}
		}
	}
	return nil
}

// departmentBlock applies the server's department verdict for the caller.
// The block map is the whole verdict: a hit naming a rule missing from the
// payload still blocks, because the server publishes the map only for
// departments it has already found over budget. When the server also lists
// the caller's candidate caps, the first one covering this call decides
// instead; a single slot cannot say whether a second, covering cap is hidden
// behind it, so it is never narrowed to the call.
func (rs ruleSet) departmentBlock(ec EvalContext) error {
	email := normalizeEmail(ec.SubscriberEmail)
	if candidates, listed := rs.departmentCandidates[email]; listed {
		return rs.departmentCandidateVerdict(candidates, ec)
	}
	ruleID, blocked := rs.departmentBlocks[email]
	if !blocked {
		return nil
	}
	balance, hasBalance := rs.departmentBalances[email]
	return rs.departmentBudgetVerdict(ruleID, balance, hasBalance, ec)
}

func (rs ruleSet) departmentCandidateVerdict(candidates []departmentCandidate, ec EvalContext) error {
	call := ec.caseFolded()
	for _, c := range candidates {
		r := rs.ruleByID(c.ruleID)
		if r != nil && !r.coversFoldedCallScope(call) {
			continue
		}
		if c.tier == candidateTierWarn {
			rs.warnNearLimitOnce(normalizeEmail(ec.SubscriberEmail), c.ruleID)
			return nil
		}
		return rs.departmentBudgetVerdict(c.ruleID, c.balance, c.hasBalance, ec)
	}
	return nil
}

func (rs ruleSet) warnNearLimitOnce(email string, ruleID int64) {
	if rs.nearLimitLogged != nil {
		if _, seen := rs.nearLimitLogged.LoadOrStore(nearLimitKey{email, ruleID}, struct{}{}); seen {
			return
		}
	}
	core.Warn("enforcement: department budget rule near its limit — rule=%d", ruleID)
}

type nearLimitKey struct {
	email  string
	ruleID int64
}

func (rs ruleSet) departmentBudgetVerdict(ruleID int64, balance float64, hasBalance bool, ec EvalContext) error {
	r := rs.ruleByID(ruleID)
	if r == nil {
		return &ErrCostLimitExceeded{
			RuleID:       strconv.FormatInt(ruleID, 10),
			RuleName:     departmentBudgetFallbackName,
			CurrentValue: balance,
			Action:       ActionBlock,
			Context:      ec,
		}
	}
	if !r.blocksAsDepartmentBudget() {
		core.Warn("enforcement: department budget rule breached (observation only) — rule=%d name=%q action=%s shadowMode=%v",
			r.RuleID, r.Name, r.Action, r.ShadowMode)
		return nil
	}
	if !hasBalance {
		balance = r.CurrentValue
	}
	return r.costLimitError(balance, ec)
}

// blocksAsDepartmentBudget treats a missing action as BLOCK: the map entry
// is already the server's blocking decision.
func (r *Rule) blocksAsDepartmentBudget() bool {
	action := r.normalizedAction()
	return !r.ShadowMode && (action == "" || action == ActionBlock)
}

func (r *Rule) costLimitError(currentValue float64, ec EvalContext) *ErrCostLimitExceeded {
	return &ErrCostLimitExceeded{
		RuleID:       strconv.FormatInt(r.RuleID, 10),
		RuleName:     r.Name,
		Threshold:    r.Threshold,
		CurrentValue: currentValue,
		PeriodType:   r.PeriodType,
		Action:       ActionBlock,
		Context:      ec,
	}
}
