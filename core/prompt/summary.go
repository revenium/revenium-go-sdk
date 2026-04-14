package prompt

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/revenium/revenium-go-sdk/core"
)

const separator = "============================================================"

func ShouldPrintSummary() string {
	val := strings.TrimSpace(strings.ToLower(os.Getenv(core.EnvPrintSummary)))
	switch val {
	case "true", "human":
		return "human"
	case "json":
		return "json"
	default:
		return ""
	}
}

func PrintUsageSummary(payload map[string]interface{}, format string) {
	switch format {
	case "json":
		fmt.Fprintln(os.Stderr, FormatJSONSummary(payload))
	case "human":
		fmt.Fprintln(os.Stderr, FormatHumanSummary(payload))
	}
}

func FormatHumanSummary(payload map[string]interface{}) string {
	var b strings.Builder

	b.WriteString(separator + "\n")
	b.WriteString("REVENIUM USAGE SUMMARY\n")
	b.WriteString(separator + "\n")

	writeField(&b, "Model", stringVal(payload, "model"))
	writeField(&b, "Provider", stringVal(payload, "provider"))

	if dur := numberVal(payload, "requestDuration"); dur > 0 {
		writeField(&b, "Duration", fmt.Sprintf("%.2fs", dur/1000))
	}

	b.WriteString("\nToken Usage:\n")
	writeField(&b, "  Input Tokens", formatInt(numberVal(payload, "inputTokenCount")))
	writeField(&b, "  Output Tokens", formatInt(numberVal(payload, "outputTokenCount")))
	writeField(&b, "  Total Tokens", formatInt(numberVal(payload, "totalTokenCount")))

	if cost := floatPtrVal(payload, "totalCost"); cost != nil {
		writeField(&b, "\nCost", fmt.Sprintf("$%.6f", *cost))
	} else {
		writeField(&b, "\nCost", "unavailable")
	}

	if traceID := stringVal(payload, "traceId"); traceID != "" {
		writeField(&b, "Trace ID", traceID)
	}

	b.WriteString(separator)
	return b.String()
}

func FormatJSONSummary(payload map[string]interface{}) string {
	summary := map[string]interface{}{
		"model":            stringVal(payload, "model"),
		"provider":         stringVal(payload, "provider"),
		"inputTokenCount":  int64(numberVal(payload, "inputTokenCount")),
		"outputTokenCount": int64(numberVal(payload, "outputTokenCount")),
		"totalTokenCount":  int64(numberVal(payload, "totalTokenCount")),
	}

	if dur := numberVal(payload, "requestDuration"); dur > 0 {
		summary["durationSeconds"] = dur / 1000
	}

	if cost := floatPtrVal(payload, "totalCost"); cost != nil {
		summary["cost"] = *cost
	} else {
		summary["cost"] = nil
		summary["costStatus"] = "unavailable"
	}

	if traceID := stringVal(payload, "traceId"); traceID != "" {
		summary["traceId"] = traceID
	}

	data, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		return "{}"
	}
	return string(data)
}

func writeField(b *strings.Builder, label, value string) {
	b.WriteString(label + ": " + value + "\n")
}

func formatInt(v float64) string {
	return fmt.Sprintf("%d", int64(v))
}

func stringVal(m map[string]interface{}, key string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func numberVal(m map[string]interface{}, key string) float64 {
	if v, ok := m[key]; ok {
		switch n := v.(type) {
		case float64:
			return n
		case int64:
			return float64(n)
		case int:
			return float64(n)
		case json.Number:
			f, _ := n.Float64()
			return f
		}
	}
	return 0
}

func floatPtrVal(m map[string]interface{}, key string) *float64 {
	if v, ok := m[key]; ok && v != nil {
		switch n := v.(type) {
		case float64:
			return &n
		case *float64:
			return n
		case json.Number:
			f, _ := n.Float64()
			return &f
		}
	}
	return nil
}
