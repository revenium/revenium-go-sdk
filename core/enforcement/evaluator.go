package enforcement

import (
	"strconv"

	"github.com/revenium/revenium-go-sdk/core"
)

// EvalContext carries the identifiers used to match enforcement rules
// against an in-flight request. Field names mirror the Node SDK so the two
// surfaces stay interchangeable.
type EvalContext struct {
	SubscriberID string
	ProductName  string
	Model        string
	Provider     string
}

// evaluate applies the cached rule set to the given context and returns
// ErrCostLimitExceeded when a BLOCK rule fires. Rules that are not
// breached, not matching the context, or configured as observation-only
// (shadowMode / WARN_ONLY / THROTTLE) do not produce an error — they only
// log when the observation-only branch fires. A nil return means "allow".
//
// This mirrors Node's enforcePreCallRules: callers should propagate the
// error without wrapping so consumers can use errors.As to recover the
// structured ErrCostLimitExceeded fields.
func evaluate(rules []Rule, ec EvalContext) error {
	for i := range rules {
		r := &rules[i]

		if !r.Breached {
			continue
		}
		if !matches(r, ec) {
			continue
		}

		if r.ShadowMode || r.Action == ActionWarnOnly || r.Action == ActionThrottle {
			core.Warn("enforcement: cost limit rule breached (observation only) — rule=%d name=%q action=%s shadowMode=%v threshold=%.4f currentValue=%.4f subscriberId=%s",
				r.RuleID, r.Name, r.Action, r.ShadowMode, r.Threshold, r.CurrentValue, ec.SubscriberID)
			continue
		}

		if r.Action == ActionBlock {
			return &ErrCostLimitExceeded{
				RuleID:       strconv.FormatInt(r.RuleID, 10),
				RuleName:     r.Name,
				Threshold:    r.Threshold,
				CurrentValue: r.CurrentValue,
				PeriodType:   r.PeriodType,
				Action:       r.Action,
				Context:      ec,
			}
		}
	}
	return nil
}

// matches returns true when the rule applies to the given evaluation
// context. An empty field on the rule means "match any" for that
// dimension, but a rule with *all* scope fields empty is rejected: it
// would silently act as a team-wide global block, turning a single
// misconfigured API response into a denial of service across every
// tenant. Server-side rules are expected to carry at least one scope
// identifier — either as a flat field or, on the v2 wire format, as an
// IS entry in `filters[]` (Rule.UnmarshalJSON back-fills the flat fields
// from those, so the same check covers both shapes).
func matches(r *Rule, ec EvalContext) bool {
	if r.SubscriberID == "" && r.ProductName == "" && r.Model == "" && r.Provider == "" {
		core.Warn("enforcement: rule %d has no scope (no subscriberId/productName/model/provider — filters=%d); skipping to avoid global-block footgun", r.RuleID, len(r.Filters))
		return false
	}
	if r.SubscriberID != "" && r.SubscriberID != ec.SubscriberID {
		return false
	}
	if r.ProductName != "" && r.ProductName != ec.ProductName {
		return false
	}
	if r.Model != "" && r.Model != ec.Model {
		return false
	}
	if r.Provider != "" && r.Provider != ec.Provider {
		return false
	}
	return true
}
