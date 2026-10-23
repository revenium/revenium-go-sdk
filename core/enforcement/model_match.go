package enforcement

import (
	"regexp"
	"strings"
)

// A MODEL filter on a call marked EvalContext.ServedModelMayBeDated is judged
// on the model the caller asked for, but the platform compares the model it
// meters, which can carry a snapshot date the request did not (OpenAI meters
// gpt-4.1-nano-2025-04-14 for gpt-4.1-nano). These forms and the rules below
// follow the Python SDK's _model_filter_matches with four differences. Python
// applies them to every call; here an unmarked call, served and recorded under
// exactly the requested name, is compared exactly, so an IS_NOT cap can exempt
// an undated fal, Runway or Ollama model the platform never counts. And, as
// decided in BACK-4063, an Azure deployment name does not match every model
// filter, a Gemini -latest alias is not expanded to versioned names, and
// xAI's served-model alias table is not applied.
const modelSnapshot = `(?:\d{4}-\d{2}-\d{2}|\d{8}|\d{4})`

var (
	modelCarriesSnapshot    = regexp.MustCompile(`-` + modelSnapshot + `(?:-|$)`)
	modelSnapshotTail       = regexp.MustCompile(`-` + modelSnapshot + `$`)
	vertexSnapshotSeparator = regexp.MustCompile(`@(` + modelSnapshot + `)$`)
)

const modelLatestSuffix = "-latest"

// IS_NOT leaves out only a call that names its own snapshot, because an
// undated call may be metered under a date the filter does not name.
func modelMatches(model string, operator Operator, expected []string) bool {
	if model == "" {
		return operator == OperatorIsNot
	}
	if operator == OperatorIsNot {
		return !modelCarriesSnapshot.MatchString(dashDated(model)) || !anyMeteredFormIs(model, dashDated(expected[0]))
	}
	for _, values := range filterSpellings(operator, expected) {
		for candidate := range meteredModelCandidates(model, values) {
			if compareModel(candidate, operator, values) {
				return true
			}
		}
	}
	return false
}

func anyMeteredFormIs(model, expected string) bool {
	_, found := meteredModelForms(model)[expected]
	return found
}

func compareModel(actual string, operator Operator, values []string) bool {
	if operator == OperatorIn {
		return anyValue(values, func(v string) bool { return actual == v })
	}
	return compare(actual, operator, values[0])
}

type modelNames map[string]struct{}

func (n modelNames) add(name string) { n[name] = struct{}{} }

func (n modelNames) each(transform func(string) (string, bool)) {
	for _, name := range n.list() {
		if transformed, ok := transform(name); ok {
			n.add(transformed)
		}
	}
}

func (n modelNames) list() []string {
	names := make([]string, 0, len(n))
	for name := range n {
		names = append(names, name)
	}
	return names
}

func meteredModelForms(model string) modelNames {
	forms := modelNames{model: {}}
	if _, unrouted, routed := strings.Cut(model, "/"); routed {
		forms.add(unrouted)
	}
	forms.each(func(form string) (string, bool) { return dashDated(form), true })
	forms.each(func(form string) (string, bool) {
		return strings.TrimSuffix(form, modelLatestSuffix), strings.HasSuffix(form, modelLatestSuffix)
	})
	return forms
}

func meteredModelCandidates(model string, expected []string) modelNames {
	candidates := meteredModelForms(model)
	for _, form := range candidates.list() {
		if modelCarriesSnapshot.MatchString(form) {
			continue
		}
		for _, value := range expected {
			if tail := modelSnapshotTail.FindString(value); tail != "" {
				candidates.add(form + tail)
			}
		}
	}
	return candidates
}

func filterSpellings(operator Operator, expected []string) [][]string {
	if operator != OperatorIs && operator != OperatorIn {
		return [][]string{expected}
	}
	dashed := make([]string, len(expected))
	changed := false
	for i, value := range expected {
		dashed[i] = dashDated(value)
		changed = changed || dashed[i] != value
	}
	if !changed {
		return [][]string{expected}
	}
	return [][]string{expected, dashed}
}

func dashDated(model string) string {
	return vertexSnapshotSeparator.ReplaceAllString(model, "-$1")
}
