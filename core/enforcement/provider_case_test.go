package enforcement

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFilterMatches_ProviderIgnoresCase(t *testing.T) {
	grok := EvalContext{Provider: "xai"}
	openAIClientXAI := EvalContext{Provider: "XAI"}
	mixed := EvalContext{Provider: "Anthropic"}

	cases := []struct {
		name   string
		filter Filter
		caller EvalContext
		want   bool
	}{
		{"IS upper rule, lower provider", isFilter(DimensionProvider, "XAI"), grok, true},
		{"IS lower rule, upper provider", isFilter(DimensionProvider, "xai"), openAIClientXAI, true},
		{"IS mixed rule, mixed provider", isFilter(DimensionProvider, "aNTHROPIC"), mixed, true},
		{"IS other provider in any case", isFilter(DimensionProvider, "openai"), openAIClientXAI, false},
		{"IS_NOT same provider other case", op(DimensionProvider, OperatorIsNot, "XAI"), grok, false},
		{"IS_NOT other provider", op(DimensionProvider, OperatorIsNot, "OPENAI"), grok, true},
		{"IN lists the provider in other case", in(DimensionProvider, "OPENAI", "XAI"), grok, true},
		{"IN lists lower case, provider upper", in(DimensionProvider, "openai", "xai"), openAIClientXAI, true},
		{"IN does not list the provider", in(DimensionProvider, "OPENAI", "GROQ"), grok, false},
		{"CONTAINS ignores case", op(DimensionProvider, OperatorContains, "THROP"), mixed, true},
		{"STARTS_WITH ignores case", op(DimensionProvider, OperatorStartsWith, "anth"), mixed, true},
		{"ENDS_WITH ignores case", op(DimensionProvider, OperatorEndsWith, "AI"), grok, true},
		{"IS anthropic covers lower-case foundry", isFilter(DimensionProvider, "anthropic"), EvalContext{Provider: "foundry"}, true},
		{"IS_NOT Anthropic excludes FOUNDRY", op(DimensionProvider, OperatorIsNot, "Anthropic"), EvalContext{Provider: "FOUNDRY"}, false},
		{"IN anthropic covers Foundry", in(DimensionProvider, "openai", "anthropic"), EvalContext{Provider: "Foundry"}, true},
		{"IS foundry still does not cover Anthropic", isFilter(DimensionProvider, "foundry"), mixed, false},
		{"IS, no provider sent", isFilter(DimensionProvider, "XAI"), EvalContext{}, false},
		{"IS_NOT, no provider sent", op(DimensionProvider, OperatorIsNot, "XAI"), EvalContext{}, true},
		{"IN, no provider sent", in(DimensionProvider, "XAI"), EvalContext{}, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.filter.matches(tc.caller))
		})
	}
}

func TestFilterMatches_OtherDimensionsKeepTheirCase(t *testing.T) {
	caller := EvalContext{Model: "grok-4", Agent: "Batch-Bot", ProductName: "Assistant", Organization: "Acme", TaskType: "Summarize", Credential: "Prod-Key"}

	for _, f := range []Filter{
		isFilter(DimensionModel, "GROK-4"),
		isFilter(DimensionAgent, "batch-bot"),
		isFilter(DimensionProduct, "assistant"),
		isFilter(DimensionOrganization, "acme"),
		isFilter(DimensionTaskType, "summarize"),
		isFilter(DimensionCredential, "prod-key"),
		in(DimensionModel, "GROK-4"),
		op(DimensionAgent, OperatorContains, "BOT"),
	} {
		assert.False(t, f.matches(caller), "%s %s %q%v", f.Dimension, f.Operator, f.Value, f.Values)
	}
}
