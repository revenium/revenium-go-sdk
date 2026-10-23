package enforcement

import (
	"encoding/json"
	"strings"
)

// Action represents what the engine should do when a rule matches and is breached.
type Action string

const (
	ActionBlock    Action = "BLOCK"
	ActionThrottle Action = "THROTTLE"
	ActionWarnOnly Action = "WARN_ONLY"
)

// Dimension is the canonical scope dimension emitted by the
// enforcement-rules endpoint inside each Filter entry and as a rule's
// groupBy. Strings match the server-side enum (Dimension.kt) so the wire
// format is the source of truth.
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
	DimensionDepartment   Dimension = "DEPARTMENT"
	DimensionOrgUnit      Dimension = "ORG_UNIT"
)

// Operator is the comparison a Filter applies to the request's value for
// its dimension.
type Operator string

const (
	OperatorIs         Operator = "IS"
	OperatorIsNot      Operator = "IS_NOT"
	OperatorIn         Operator = "IN"
	OperatorContains   Operator = "CONTAINS"
	OperatorStartsWith Operator = "STARTS_WITH"
	OperatorEndsWith   Operator = "ENDS_WITH"
)

const metricTotalCost = "TOTAL_COST"

// Filter is one entry of the rule's `filters[]` array. Multiple filters
// on a single rule compose with AND. Value carries the operand of the
// scalar operators; Values carries the list of an IN filter.
type Filter struct {
	Dimension Dimension `json:"dimension"`
	Operator  Operator  `json:"operator"`
	Value     string    `json:"value"`
	Values    []string  `json:"values,omitempty"`

	valueSent bool
}

// UnmarshalJSON records whether the payload carried a value at all, because
// the platform reads a null value as "constrains nothing" while an empty
// string is still a value to compare against.
func (f *Filter) UnmarshalJSON(data []byte) error {
	type filterAlias Filter
	var raw struct {
		filterAlias
		Value *string `json:"value"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*f = Filter(raw.filterAlias)
	if raw.Value != nil {
		f.Value = *raw.Value
		f.valueSent = true
	}
	return nil
}

func (f Filter) hasValue() bool {
	return f.valueSent || f.Value != ""
}

// GroupEntry is one person's (or one department's) balance under a
// grouped rule, as published in the rule's `groupBreakdown`.
type GroupEntry struct {
	GroupValue   string  `json:"groupValue"`
	CurrentValue float64 `json:"currentValue"`
	Breached     bool    `json:"breached"`
}

// Rule mirrors the compiled enforcement rule returned by
// GET /v2/api/ai/enforcement-rules/{teamIdHashed}. Server-side the ruleId is
// numeric; it is stringified when surfaced to callers through
// ErrCostLimitExceeded to match the Node SDK's public contract.
type Rule struct {
	RuleID       int64   `json:"ruleId"`
	Name         string  `json:"name,omitempty"`
	MetricType   string  `json:"metricType,omitempty"`
	Threshold    float64 `json:"threshold"`
	CurrentValue float64 `json:"currentValue"`
	PeriodType   string  `json:"periodType,omitempty"`
	Action       Action  `json:"action"`
	Breached     bool    `json:"breached"`
	ShadowMode   bool    `json:"shadowMode"`

	Filters []Filter `json:"filters,omitempty"`

	// GroupBy splits the rule's threshold per group: SUBSCRIBER makes it a
	// per-person cap judged on each person's own entry in GroupBreakdown;
	// DEPARTMENT makes it a department budget decided by the server.
	GroupBy        Dimension    `json:"groupBy,omitempty"`
	GroupBreakdown []GroupEntry `json:"groupBreakdown,omitempty"`

	// DepartmentID is set when the rule is a budget scoped to one
	// department. Only the server knows who belongs to it, so such a rule
	// blocks a caller only through the payload's department block map.
	DepartmentID *int64 `json:"departmentId,omitempty"`
}

// UnmarshalJSON accepts the department field under the `orgUnitId`
// spelling the platform served before the department rename.
func (r *Rule) UnmarshalJSON(data []byte) error {
	type ruleAlias Rule
	var raw struct {
		ruleAlias
		OrgUnitID *int64 `json:"orgUnitId"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*r = Rule(raw.ruleAlias)
	if r.DepartmentID == nil {
		r.DepartmentID = raw.OrgUnitID
	}
	return nil
}

func (r *Rule) groupedBy(dimensions ...Dimension) bool {
	groupBy := Dimension(strings.ToUpper(strings.TrimSpace(string(r.GroupBy))))
	for _, d := range dimensions {
		if groupBy == d {
			return true
		}
	}
	return false
}

// isDepartmentRule mirrors the platform's exclusion of department rules
// from per-rule evaluation: their rule-level breached flag means "some
// department is over budget", never "this caller is".
func (r *Rule) isDepartmentRule() bool {
	return r.DepartmentID != nil || r.groupedBy(DimensionDepartment, DimensionOrgUnit)
}

func (r *Rule) measuresCost() bool {
	return r.MetricType == "" || strings.EqualFold(r.MetricType, metricTotalCost)
}

func (r *Rule) normalizedAction() Action {
	return Action(strings.ToUpper(strings.TrimSpace(string(r.Action))))
}

func (r *Rule) isObservationOnly() bool {
	action := r.normalizedAction()
	return r.ShadowMode || action == ActionWarnOnly || action == ActionThrottle
}
