package enforcement

import (
	"strings"

	"github.com/revenium/revenium-go-sdk/core"
)

// The matcher mirrors the platform's EnforcementFilterMatcher so a rule
// covers exactly the requests the server would count against it. An empty
// request value stands for the platform's null.
// The one exception is a MODEL filter on a call marked
// EvalContext.ServedModelMayBeDated: it follows the Python SDK's matcher
// (model_match.go) and may cover calls the platform records under the
// requested, undated name.

const (
	providerAnthropic = core.ProviderNameAnthropic
	providerFoundry   = "FOUNDRY"
)

// providerAliases lists, for each aliased provider, every PROVIDER filter
// value its traffic satisfies, matching the platform's ProviderVocabulary.
var providerAliases = map[string][]string{
	providerFoundry: {providerFoundry, providerAnthropic},
}

// covers reports whether every filter in the rule's scope matches the
// request. A rule with no filters covers every request.
func (r *Rule) covers(ec EvalContext) bool {
	for _, f := range r.Filters {
		if !f.matches(ec) {
			return false
		}
	}
	return true
}

// coversFoldedCallScope judges a department candidate's filters against the
// call the way the server counted the cap's total: letter case ignored, and a
// row the SDK cannot evaluate taken as matching, because the server already
// found this caller over the cap. Department, org-unit and subscriber rows
// were settled by the server when it listed the candidate. A credential row
// also counts as matching when the call names its credential only by raw
// value: the server counted the total under the credential's saved name,
// which a raw value cannot be matched to. The call arrives already passed
// through caseFolded, so a scan over several candidates folds it once.
func (r *Rule) coversFoldedCallScope(call EvalContext) bool {
	for _, f := range r.Filters {
		if f.settledByServer() || !f.evaluable() || (f.Dimension == DimensionCredential && call.credentialValueOnly) {
			continue
		}
		if !f.caseFolded().matches(call) {
			return false
		}
	}
	return true
}

func (f Filter) settledByServer() bool {
	switch f.Dimension {
	case DimensionDepartment, DimensionOrgUnit, DimensionSubscriber:
		return true
	}
	return false
}

func (f Filter) caseFolded() Filter {
	f.Value = strings.ToLower(f.Value)
	values := make([]string, len(f.Values))
	for i, v := range f.Values {
		values[i] = strings.ToLower(v)
	}
	f.Values = values
	return f
}

var supportedOperators = map[Operator]bool{
	OperatorIs: true, OperatorIsNot: true, OperatorIn: true,
	OperatorContains: true, OperatorStartsWith: true, OperatorEndsWith: true,
}

// evaluable reports whether the SDK can judge the filter at all. A filter
// on a dimension or with an operator it does not know never matches, even
// with no value to compare, so such a rule is left to the platform rather
// than widened to every request.
func (f Filter) evaluable() bool {
	if !supportedOperators[f.Operator] {
		return false
	}
	switch f.Dimension {
	case DimensionProvider, DimensionSubscriber:
		return true
	}
	_, known := EvalContext{}.valueOf(f.Dimension)
	return known
}

func (f Filter) matches(ec EvalContext) bool {
	if !f.evaluable() {
		return false
	}
	if f.Operator == OperatorIn {
		return f.matchesAnyValue(ec)
	}
	if !f.hasValue() {
		return true
	}
	switch f.Dimension {
	case DimensionProvider:
		return compareProvider(ec.Provider, f.Operator, f.Value)
	case DimensionSubscriber:
		return compareSubscriber(ec.subscriberValue(), f.Operator, f.Value)
	case DimensionModel:
		if ec.servedModelMayBeDated {
			return modelMatches(ec.Model, f.Operator, []string{f.Value})
		}
	}
	actual, _ := ec.valueOf(f.Dimension)
	return compare(actual, f.Operator, f.Value)
}

func (f Filter) matchesAnyValue(ec EvalContext) bool {
	if len(f.Values) == 0 {
		return true
	}
	switch f.Dimension {
	case DimensionProvider:
		return anyValue(f.Values, func(v string) bool { return providerEquals(ec.Provider, v) })
	case DimensionSubscriber:
		subscriber := ec.subscriberValue()
		return anyValue(f.Values, func(v string) bool { return subscriber.equals(v) })
	case DimensionModel:
		if ec.servedModelMayBeDated {
			return modelMatches(ec.Model, OperatorIn, f.Values)
		}
	}
	actual, _ := ec.valueOf(f.Dimension)
	return anyValue(f.Values, func(v string) bool { return actual != "" && actual == v })
}

func anyValue(values []string, match func(string) bool) bool {
	for _, v := range values {
		if match(v) {
			return true
		}
	}
	return false
}

func compare(actual string, operator Operator, expected string) bool {
	if actual == "" {
		return operator == OperatorIsNot
	}
	switch operator {
	case OperatorIs:
		return actual == expected
	case OperatorIsNot:
		return actual != expected
	case OperatorContains:
		return strings.Contains(actual, expected)
	case OperatorStartsWith:
		return strings.HasPrefix(actual, expected)
	case OperatorEndsWith:
		return strings.HasSuffix(actual, expected)
	default:
		return false
	}
}

// Upper case, not lower: the providerAliases keys and values are upper case,
// so the Foundry-to-Anthropic alias only matches in this form.
func canonicalProvider(provider string) string {
	return strings.ToUpper(provider)
}

func providerViaAlias(actual, expected string) bool {
	if actual == "" {
		return false
	}
	for _, satisfied := range providerAliases[canonicalProvider(actual)] {
		if satisfied == canonicalProvider(expected) {
			return true
		}
	}
	return false
}

func providerEquals(actual, expected string) bool {
	return actual != "" && compareProvider(actual, OperatorIs, expected)
}

func compareProvider(actual string, operator Operator, expected string) bool {
	if providerViaAlias(actual, expected) {
		switch operator {
		case OperatorIs:
			return true
		case OperatorIsNot:
			return false
		}
	}
	return compare(canonicalProvider(actual), operator, canonicalProvider(expected))
}

// subscriberValue is what a SUBSCRIBER filter reads: the email when one is
// present, else the id. Only an email is one identity across letter case.
type subscriberValue struct {
	value     string
	fromEmail bool
}

func (s subscriberValue) equals(expected string) bool {
	if s.value == "" {
		return false
	}
	if s.fromEmail {
		return normalizeEmail(s.value) == normalizeEmail(expected)
	}
	return s.value == expected
}

func compareSubscriber(actual subscriberValue, operator Operator, expected string) bool {
	if !actual.fromEmail {
		return compare(actual.value, operator, expected)
	}
	switch operator {
	case OperatorIs, OperatorIsNot:
		return compare(normalizeEmail(actual.value), operator, normalizeEmail(expected))
	default:
		return compare(strings.ToLower(actual.value), operator, strings.ToLower(expected))
	}
}

// normalizeEmail is the platform's canonical address form (EmailNormalizer):
// trimmed and lower-cased.
func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}
