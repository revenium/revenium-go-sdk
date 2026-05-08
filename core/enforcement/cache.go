package enforcement

import (
	"encoding/json"
	"sync"
	"time"
)

// Action represents what the engine should do when a rule matches and is breached.
type Action string

const (
	ActionBlock    Action = "BLOCK"
	ActionThrottle Action = "THROTTLE"
	ActionWarnOnly Action = "WARN_ONLY"
)

// Dimension is the canonical scope dimension emitted by the
// enforcement-rules endpoint inside each Filter entry. Strings match the
// server-side enum (Dimension.kt) so the wire format is the source of
// truth.
type Dimension string

const (
	DimensionModel        Dimension = "MODEL"
	DimensionSubscriber   Dimension = "SUBSCRIBER"
	DimensionProduct      Dimension = "PRODUCT"
	DimensionProvider     Dimension = "PROVIDER"
	DimensionOrganization Dimension = "ORGANIZATION"
	DimensionCredential   Dimension = "CREDENTIAL"
	DimensionAgent        Dimension = "AGENT"
	DimensionTaskType     Dimension = "TASK_TYPE"
)

// Operator is the comparison applied to a Filter's value. The evaluator
// only honours IS for scope derivation today; the other operators are
// preserved on the wire so future evaluator work can use them without
// another shape change.
type Operator string

const (
	OperatorIs         Operator = "IS"
	OperatorIsNot      Operator = "IS_NOT"
	OperatorContains   Operator = "CONTAINS"
	OperatorStartsWith Operator = "STARTS_WITH"
	OperatorEndsWith   Operator = "ENDS_WITH"
)

// Filter is one entry of the rule's `filters[]` array. Multiple filters
// on a single rule compose with AND.
type Filter struct {
	Dimension Dimension `json:"dimension"`
	Operator  Operator  `json:"operator"`
	Value     string    `json:"value"`
}

// Rule mirrors the compiled enforcement rule returned by
// GET /v2/api/ai/enforcement-rules/{teamIdHashed}. Server-side the ruleId is
// numeric; it is stringified when surfaced to callers through
// ErrCostLimitExceeded to match the Node SDK's public contract.
type Rule struct {
	RuleID       int64   `json:"ruleId"`
	Name         string  `json:"name,omitempty"`
	Threshold    float64 `json:"threshold"`
	CurrentValue float64 `json:"currentValue"`
	PercentUsed  float64 `json:"percentUsed,omitempty"`
	PeriodType   string  `json:"periodType,omitempty"`
	Action       Action  `json:"action"`
	Breached     bool    `json:"breached"`
	ShadowMode   bool    `json:"shadowMode"`

	// Filters carries the server's wire-shape scope. UnmarshalJSON
	// back-fills the flat fields below from any IS filter so the
	// equality-based evaluator works without further plumbing.
	Filters []Filter `json:"filters,omitempty"`

	// Scope fields — an empty field means "match any" for that dimension.
	// A rule with all four empty is skipped (match-all is a footgun).
	SubscriberID string `json:"subscriberId,omitempty"`
	ProductName  string `json:"productName,omitempty"`
	Model        string `json:"model,omitempty"`
	Provider     string `json:"provider,omitempty"`
}

// UnmarshalJSON decodes the wire payload, then back-fills the flat scope
// fields from any IS filters. The v2 enforcement-rules endpoint emits
// scope inside `filters[]` rather than as top-level fields, so without
// this derivation every server-managed rule would be skipped by the
// evaluator's no-scope guard. Flat fields already present in the payload
// win over filter-derived values, which keeps tests and any
// legacy-shape responses working unchanged.
func (r *Rule) UnmarshalJSON(data []byte) error {
	type ruleAlias Rule
	var raw ruleAlias
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*r = Rule(raw)
	for _, f := range r.Filters {
		if f.Operator != "" && f.Operator != OperatorIs {
			continue
		}
		switch f.Dimension {
		case DimensionModel:
			if r.Model == "" {
				r.Model = f.Value
			}
		case DimensionSubscriber:
			if r.SubscriberID == "" {
				r.SubscriberID = f.Value
			}
		case DimensionProduct:
			if r.ProductName == "" {
				r.ProductName = f.Value
			}
		case DimensionProvider:
			if r.Provider == "" {
				r.Provider = f.Value
			}
		}
	}
	return nil
}

// cache is a thread-safe in-memory store for enforcement rules.
type cache struct {
	mu        sync.RWMutex
	rules     []Rule
	updatedAt time.Time
}

func newCache() *cache {
	return &cache{}
}

// update replaces the entire rule set atomically.
func (c *cache) update(rules []Rule) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.rules = rules
	c.updatedAt = time.Now()
}

// snapshot returns a shallow copy of the current rules so callers can
// iterate without holding the lock.
func (c *cache) snapshot() []Rule {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]Rule, len(c.rules))
	copy(out, c.rules)
	return out
}

// lastUpdated returns when the cache was last refreshed.
func (c *cache) lastUpdated() time.Time {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.updatedAt
}
