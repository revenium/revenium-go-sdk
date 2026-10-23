package enforcement

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"

	"github.com/revenium/revenium-go-sdk/core"
)

// ruleSet is one decoded enforcement payload: the team's rules plus the
// server's department-budget verdicts, keyed by normalized subscriber email.
type ruleSet struct {
	rules                []Rule
	departmentBlocks     map[string]int64
	departmentBalances   map[string]float64
	departmentCandidates map[string][]departmentCandidate
	nearLimitLogged      *sync.Map
}

type candidateTier string

const (
	candidateTierBlock candidateTier = "BLOCK"
	candidateTierWarn  candidateTier = "WARN"
)

// departmentCandidate is one department cap the server found the caller
// over or near, listed best-first. The server publishes candidates only when
// one of them is scoped to part of the caller's calls, so the SDK picks the
// first that covers the call instead of applying the single block slot to all.
type departmentCandidate struct {
	ruleID     int64
	tier       candidateTier
	balance    float64
	hasBalance bool
}

// rulesEnvelope is the `{rules: [...]}` body. Each department map is read
// under its current name first and its pre-rename org-unit name second.
type rulesEnvelope struct {
	Rules                         []Rule                     `json:"rules"`
	DepartmentBudgetBlocks        map[string]json.RawMessage `json:"departmentBudgetBlocks"`
	OrgUnitBudgetBlocks           map[string]json.RawMessage `json:"orgUnitBudgetBlocks"`
	DepartmentBudgetBlockBalances map[string]json.RawMessage `json:"departmentBudgetBlockBalances"`
	OrgUnitBudgetBlockBalances    map[string]json.RawMessage `json:"orgUnitBudgetBlockBalances"`
	DepartmentBudgetCandidates    map[string]json.RawMessage `json:"departmentBudgetCandidates"`
}

// decodeRuleSet accepts the envelope the API serves and, for forward
// compatibility, a bare array of rules.
func decodeRuleSet(body []byte) (ruleSet, error) {
	var envelope rulesEnvelope
	envelopeErr := json.Unmarshal(body, &envelope)
	if envelopeErr == nil && envelope.Rules != nil {
		return ruleSet{
			rules:                envelope.Rules,
			departmentBlocks:     ruleIDsByEmail(firstPresent(envelope.DepartmentBudgetBlocks, envelope.OrgUnitBudgetBlocks)),
			departmentBalances:   balancesByEmail(firstPresent(envelope.DepartmentBudgetBlockBalances, envelope.OrgUnitBudgetBlockBalances)),
			departmentCandidates: candidatesByEmail(envelope.DepartmentBudgetCandidates),
			nearLimitLogged:      &sync.Map{},
		}, nil
	}
	var rules []Rule
	if arrayErr := json.Unmarshal(body, &rules); arrayErr != nil {
		return ruleSet{}, fmt.Errorf("decode rules: envelope=%v array=%w", envelopeErr, arrayErr)
	}
	return ruleSet{rules: rules}, nil
}

func firstPresent(current, legacy map[string]json.RawMessage) map[string]json.RawMessage {
	if current != nil {
		return current
	}
	return legacy
}

// ruleIDsByEmail keeps only entries whose value is an integral rule id, so
// one malformed entry costs that entry rather than the whole rule set.
func ruleIDsByEmail(raw map[string]json.RawMessage) map[string]int64 {
	out := make(map[string]int64, len(raw))
	for email, value := range raw {
		id, err := strconv.ParseInt(strings.TrimSpace(string(value)), 10, 64)
		if err != nil {
			core.Debug("enforcement: dropping department block entry for %q, value %q is not a rule id", email, string(value))
			continue
		}
		putFirst(out, normalizeEmail(email), id)
	}
	return out
}

// balancesByEmail accepts a balance serialized as a JSON number or string,
// as a BigDecimal may be, and drops anything that is not a finite amount.
func balancesByEmail(raw map[string]json.RawMessage) map[string]float64 {
	out := make(map[string]float64, len(raw))
	for email, value := range raw {
		balance, ok := parseAmount(value)
		if !ok {
			core.Debug("enforcement: dropping department balance entry for %q, value %q is not a finite amount", email, string(value))
			continue
		}
		putFirst(out, normalizeEmail(email), balance)
	}
	return out
}

// candidatesByEmail drops malformed candidates one by one and leaves out an
// email with none left, so that caller keeps the single-slot verdict.
func candidatesByEmail(raw map[string]json.RawMessage) map[string][]departmentCandidate {
	out := make(map[string][]departmentCandidate, len(raw))
	for email, value := range raw {
		var items []json.RawMessage
		if err := json.Unmarshal(value, &items); err != nil {
			core.Debug("enforcement: dropping a department candidate list, value %q is not a list", string(value))
			continue
		}
		candidates := make([]departmentCandidate, 0, len(items))
		for _, item := range items {
			candidate, ok := parseCandidate(item)
			if !ok {
				core.Debug("enforcement: dropping a department candidate, value %q is not a candidate", string(item))
				continue
			}
			candidates = append(candidates, candidate)
		}
		if len(candidates) > 0 {
			putFirst(out, normalizeEmail(email), candidates)
		}
	}
	return out
}

func parseCandidate(item json.RawMessage) (departmentCandidate, bool) {
	var raw struct {
		RuleID  json.RawMessage `json:"ruleId"`
		Tier    string          `json:"tier"`
		Balance json.RawMessage `json:"balance"`
	}
	if err := json.Unmarshal(item, &raw); err != nil {
		return departmentCandidate{}, false
	}
	ruleID, err := strconv.ParseInt(unquotedNumber(raw.RuleID), 10, 64)
	if err != nil || ruleID <= 0 {
		return departmentCandidate{}, false
	}
	tier := candidateTier(strings.ToUpper(strings.TrimSpace(raw.Tier)))
	if tier != candidateTierBlock && tier != candidateTierWarn {
		return departmentCandidate{}, false
	}
	balance, hasBalance := parseAmount(raw.Balance)
	return departmentCandidate{ruleID: ruleID, tier: tier, balance: balance, hasBalance: hasBalance}, true
}

// unquotedNumber reads a JSON number that a serializer may have written as a
// string, which Jackson accepts on the way in and so can appear on the wire.
func unquotedNumber(value json.RawMessage) string {
	text := strings.TrimSpace(string(value))
	if unquoted, err := strconv.Unquote(text); err == nil {
		return unquoted
	}
	return text
}

func parseAmount(value json.RawMessage) (float64, bool) {
	amount, err := strconv.ParseFloat(unquotedNumber(value), 64)
	if err != nil || math.IsNaN(amount) || math.IsInf(amount, 0) {
		return 0, false
	}
	return amount, true
}

func putFirst[V any](m map[string]V, key string, value V) {
	if key == "" {
		return
	}
	if _, taken := m[key]; taken {
		return
	}
	m[key] = value
}

func (rs ruleSet) ruleByID(id int64) *Rule {
	for i := range rs.rules {
		if rs.rules[i].RuleID == id {
			return &rs.rules[i]
		}
	}
	return nil
}
