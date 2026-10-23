package enforcement

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestModelFilter_JudgesACallServedModelMayBeDatedOnEveryNameThePlatformMayMeter(t *testing.T) {
	cases := []struct {
		name   string
		filter Filter
		model  string
		want   bool
	}{
		{"IS a dated name covers the undated call metered under that date", isFilter(DimensionModel, "gpt-4.1-nano-2025-04-14"), "gpt-4.1-nano", true},
		{"IS a dated name with an eight-digit date", isFilter(DimensionModel, "claude-haiku-4-5-20251001"), "claude-haiku-4-5", true},
		{"IS a dated name does not cover another model", isFilter(DimensionModel, "gpt-4.1-nano-2025-04-14"), "gpt-4o", false},
		{"IS a dated name does not cover a call naming another snapshot", isFilter(DimensionModel, "gpt-4o-2024-08-06"), "gpt-4o-2024-05-13", false},
		{"IS the undated name covers the undated call", isFilter(DimensionModel, "gpt-4.1-nano"), "gpt-4.1-nano", true},
		{"IS the undated name does not cover a call naming a snapshot", isFilter(DimensionModel, "gpt-4.1-nano"), "gpt-4.1-nano-2025-04-14", false},
		{"IS covers a LiteLLM-style routed call by its model name", isFilter(DimensionModel, "gpt-4.1-nano"), "openai/gpt-4.1-nano", true},
		{"IS covers a -latest alias by its base name", isFilter(DimensionModel, "claude-3-7-sonnet"), "claude-3-7-sonnet-latest", true},
		{"IS a dash-dated name covers a Vertex @ snapshot", isFilter(DimensionModel, "claude-haiku-4-5-20251001"), "claude-haiku-4-5@20251001", true},
		{"IS a Vertex @ snapshot covers the dash-dated call", isFilter(DimensionModel, "claude-haiku-4-5@20251001"), "claude-haiku-4-5-20251001", true},
		{"IS a Vertex @ snapshot covers the same @ call", isFilter(DimensionModel, "claude-haiku-4-5@20251001"), "claude-haiku-4-5@20251001", true},
		{"IS a dash-dated name covers the same dash-dated call", isFilter(DimensionModel, "claude-haiku-4-5-20251001"), "claude-haiku-4-5-20251001", true},
		{"IS_NOT a Vertex @ snapshot leaves the call naming it alone", op(DimensionModel, OperatorIsNot, "claude-haiku-4-5@20251001"), "claude-haiku-4-5@20251001", false},
		{"IS_NOT a dash-dated name leaves the @ call for the same snapshot alone", op(DimensionModel, OperatorIsNot, "claude-haiku-4-5-20251001"), "claude-haiku-4-5@20251001", false},
		{"IS_NOT a Vertex @ snapshot leaves the dash-dated call for the same snapshot alone", op(DimensionModel, OperatorIsNot, "claude-haiku-4-5@20251001"), "claude-haiku-4-5-20251001", false},
		{"IS_NOT a Vertex @ snapshot covers another snapshot", op(DimensionModel, OperatorIsNot, "claude-haiku-4-5@20251001"), "claude-haiku-4-5@20250101", true},
		{"IS_NOT a Vertex @ snapshot covers the undated call", op(DimensionModel, OperatorIsNot, "claude-haiku-4-5@20251001"), "claude-haiku-4-5", true},
		{"IN a dated name covers the undated call", in(DimensionModel, "gpt-4o", "gpt-4.1-nano-2025-04-14"), "gpt-4.1-nano", true},
		{"IN without the model passes", in(DimensionModel, "gpt-4o-2024-08-06"), "gpt-4.1-nano", false},
		{"IS_NOT the undated name covers the undated call, metered under a date", op(DimensionModel, OperatorIsNot, "gpt-4.1-nano"), "gpt-4.1-nano", true},
		{"IS_NOT a dated name covers the undated call", op(DimensionModel, OperatorIsNot, "gpt-4.1-nano-2025-04-14"), "gpt-4.1-nano", true},
		{"IS_NOT the dated name leaves the call naming it alone", op(DimensionModel, OperatorIsNot, "gpt-4.1-nano-2025-04-14"), "gpt-4.1-nano-2025-04-14", false},
		{"IS_NOT the undated name covers a call naming a snapshot", op(DimensionModel, OperatorIsNot, "gpt-4.1-nano"), "gpt-4.1-nano-2025-04-14", true},
		{"IS_NOT another model covers a dated call", op(DimensionModel, OperatorIsNot, "gpt-4o"), "gpt-4.1-nano-2025-04-14", true},
		{"ENDS_WITH a date covers the undated call metered under it", op(DimensionModel, OperatorEndsWith, "nano-2025-04-14"), "gpt-4.1-nano", true},
		{"STARTS_WITH a family covers a routed call", op(DimensionModel, OperatorStartsWith, "gpt-4o"), "openai/gpt-4o-mini", true},
		{"CONTAINS misses another family", op(DimensionModel, OperatorContains, "claude"), "gpt-4o", false},
		{"IS_NOT covers a call that names no model", op(DimensionModel, OperatorIsNot, "gpt-4o"), "", true},
		{"IS never covers a call that names no model", isFilter(DimensionModel, "gpt-4o-2024-08-06"), "", false},
		{"IN never covers a call that names no model", in(DimensionModel, "gpt-4o"), "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.filter.matches(EvalContext{Model: tc.model}.ServedModelMayBeDated()))
		})
	}
}

func TestModelFilter_ComparesACallRecordedUnderTheRequestedNameExactly(t *testing.T) {
	cases := []struct {
		name   string
		filter Filter
		model  string
		want   bool
	}{
		{"IS_NOT the fal endpoint leaves a call to it alone", op(DimensionModel, OperatorIsNot, "fal_ai/fal-ai/flux/dev"), "fal_ai/fal-ai/flux/dev", false},
		{"IS_NOT the Runway model leaves a call to it alone", op(DimensionModel, OperatorIsNot, "gen4_turbo"), "gen4_turbo", false},
		{"IS_NOT an undated Ollama model leaves a call to it alone", op(DimensionModel, OperatorIsNot, "llama3.1:8b"), "llama3.1:8b", false},
		{"IS_NOT another model covers the call", op(DimensionModel, OperatorIsNot, "gen4_turbo"), "gen3a_turbo", true},
		{"IS the same name covers the call", isFilter(DimensionModel, "gen4_turbo"), "gen4_turbo", true},
		{"IS a dated name does not cover an undated call", isFilter(DimensionModel, "gpt-4.1-nano-2025-04-14"), "gpt-4.1-nano", false},
		{"IS an unrouted name does not cover the routed call", isFilter(DimensionModel, "fal-ai/flux/dev"), "fal_ai/fal-ai/flux/dev", false},
		{"IN a dated name does not cover an undated call", in(DimensionModel, "gpt-4.1-nano-2025-04-14"), "gpt-4.1-nano", false},
		{"IN the same name covers the call", in(DimensionModel, "gen3a_turbo", "gen4_turbo"), "gen4_turbo", true},
		{"IS_NOT covers a call that names no model", op(DimensionModel, OperatorIsNot, "gen4_turbo"), "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.filter.matches(EvalContext{Model: tc.model}))
		})
	}
}
