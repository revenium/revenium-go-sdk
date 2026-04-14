package prompt

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestShouldPrintSummary(t *testing.T) {
	tests := []struct {
		env    string
		expect string
	}{
		{"", ""},
		{"true", "human"},
		{"TRUE", "human"},
		{"human", "human"},
		{"json", "json"},
		{"JSON", "json"},
		{"false", ""},
		{"disabled", ""},
	}

	for _, tt := range tests {
		t.Run(tt.env, func(t *testing.T) {
			t.Setenv("REVENIUM_PRINT_SUMMARY", tt.env)
			assert.Equal(t, tt.expect, ShouldPrintSummary())
		})
	}
}

func samplePayload() map[string]interface{} {
	return map[string]interface{}{
		"model":            "gpt-4",
		"provider":         "OpenAI",
		"requestDuration":  float64(2500),
		"inputTokenCount":  float64(100),
		"outputTokenCount": float64(200),
		"totalTokenCount":  float64(300),
		"totalCost":        float64(0.003456),
		"traceId":          "trace-abc-123",
	}
}

func TestFormatHumanSummary(t *testing.T) {
	result := FormatHumanSummary(samplePayload())

	assert.Contains(t, result, "REVENIUM USAGE SUMMARY")
	assert.Contains(t, result, "Model: gpt-4")
	assert.Contains(t, result, "Provider: OpenAI")
	assert.Contains(t, result, "Duration: 2.50s")
	assert.Contains(t, result, "Input Tokens: 100")
	assert.Contains(t, result, "Output Tokens: 200")
	assert.Contains(t, result, "Total Tokens: 300")
	assert.Contains(t, result, "Cost: $0.003456")
	assert.Contains(t, result, "Trace ID: trace-abc-123")
	assert.True(t, strings.HasPrefix(result, separator))
	assert.True(t, strings.HasSuffix(result, separator))
}

func TestFormatHumanSummary_NoCost(t *testing.T) {
	p := samplePayload()
	delete(p, "totalCost")
	result := FormatHumanSummary(p)
	assert.Contains(t, result, "Cost: unavailable")
}

func TestFormatJSONSummary(t *testing.T) {
	result := FormatJSONSummary(samplePayload())

	var parsed map[string]interface{}
	err := json.Unmarshal([]byte(result), &parsed)
	require.NoError(t, err)

	assert.Equal(t, "gpt-4", parsed["model"])
	assert.Equal(t, "OpenAI", parsed["provider"])
	assert.Equal(t, float64(100), parsed["inputTokenCount"])
	assert.Equal(t, float64(200), parsed["outputTokenCount"])
	assert.Equal(t, float64(300), parsed["totalTokenCount"])
	assert.Equal(t, 0.003456, parsed["cost"])
	assert.Equal(t, "trace-abc-123", parsed["traceId"])
	assert.InDelta(t, 2.5, parsed["durationSeconds"], 0.01)
}

func TestFormatJSONSummary_NoCost(t *testing.T) {
	p := samplePayload()
	delete(p, "totalCost")
	result := FormatJSONSummary(p)

	var parsed map[string]interface{}
	err := json.Unmarshal([]byte(result), &parsed)
	require.NoError(t, err)

	assert.Nil(t, parsed["cost"])
	assert.Equal(t, "unavailable", parsed["costStatus"])
}
