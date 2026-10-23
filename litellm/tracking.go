package litellm

import (
	"math"
	"net/http"
	"strconv"
	"strings"
)

var headerMetadataFields = map[string]string{
	"X-Revenium-Product-Name":          "productName",
	"X-Revenium-Product-Id":            "productName",
	"X-Revenium-Organization-Name":     "organizationName",
	"X-Revenium-Organization-Id":       "organizationName",
	"X-Revenium-Trace-Id":              "traceId",
	"X-Revenium-Task-Type":             "taskType",
	"X-Revenium-Agent":                 "agent",
	"X-Revenium-Subscription-Id":       "subscriptionId",
	"X-Revenium-Environment":           "environment",
	"X-Revenium-Operation-Subtype":     "operationSubtype",
	"X-Revenium-Parent-Transaction-Id": "parentTransactionId",
	"X-Revenium-Transaction-Name":      "transactionName",
	"X-Revenium-Region":                "region",
	"X-Revenium-Credential-Alias":      "credentialAlias",
	"X-Revenium-Trace-Type":            "traceType",
	"X-Revenium-Trace-Name":            "traceName",
	"X-Revenium-Ticket-Id":             "ticketId",
	"X-Revenium-Agentic-Job-Id":        "agenticJobId",
	"X-Revenium-Agentic-Job-Name":      "agenticJobName",
	"X-Revenium-Agentic-Job-Type":      "agenticJobType",
	"X-Revenium-Agentic-Job-Version":   "agenticJobVersion",
}

var subscriberHeaderFields = map[string]string{
	"X-Revenium-Subscriber-Id":    "id",
	"X-Revenium-Subscriber-Email": "email",
}

var subscriberCredentialHeaderFields = map[string]string{
	"X-Revenium-Subscriber-Credential-Name": "name",
	"X-Revenium-Subscriber-Credential":      "value",
}

// ExtractMetadataFromHeaders maps Revenium-prefixed HTTP headers to the usage metadata shape.
// X-Revenium-Capture-Prompts is not mapped: only the caller decides whether prompts are captured.
func ExtractMetadataFromHeaders(headers http.Header) map[string]interface{} {
	if headers == nil {
		return nil
	}
	out := headerValues(headers, headerMetadataFields)
	if subscriber := subscriberFromHeaders(headers); len(subscriber) > 0 {
		out["subscriber"] = subscriber
	}
	if v := filledHeader(headers, "X-Revenium-Organization-Name"); v != "" {
		out["organizationName"] = v
	}
	if v := filledHeader(headers, "X-Revenium-Product-Name"); v != "" {
		out["productName"] = v
	}
	if v := filledHeader(headers, "X-Revenium-Retry-Number"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			out["retryNumber"] = n
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// subscriberFromHeaders builds the nested subscriber object the usage record
// carries, since the record has no flat subscriber fields
func subscriberFromHeaders(headers http.Header) map[string]interface{} {
	subscriber := headerValues(headers, subscriberHeaderFields)
	if credential := headerValues(headers, subscriberCredentialHeaderFields); len(credential) > 0 {
		subscriber["credential"] = credential
	}
	return subscriber
}

func headerValues(headers http.Header, fields map[string]string) map[string]interface{} {
	out := map[string]interface{}{}
	for header, field := range fields {
		if v := filledHeader(headers, header); v != "" {
			out[field] = v
		}
	}
	return out
}

const captureSwitch = "capturePrompts"

// meteredMetadata is the caller's usage metadata merged over what the proxy's response
// headers carry. The prompt capture switch stays exactly as the caller set it, even blank.
func meteredMetadata(callerMetadata map[string]interface{}, headers http.Header) map[string]interface{} {
	merged := MergeMetadata(callerMetadata, ExtractMetadataFromHeaders(headers))
	if value, set := callerMetadata[captureSwitch]; set {
		merged[captureSwitch] = value
	}
	return merged
}

// MergeMetadata merges header-derived metadata under context-derived metadata
// context values take precedence, except a blank string, which leaves the
// header value in place; nil context is treated as empty
func MergeMetadata(contextMeta, headerMeta map[string]interface{}) map[string]interface{} {
	if len(contextMeta) == 0 && len(headerMeta) == 0 {
		return nil
	}
	merged := make(map[string]interface{}, len(contextMeta)+len(headerMeta))
	for k, v := range headerMeta {
		merged[k] = v
	}
	for k, v := range contextMeta {
		if isBlankString(v) {
			continue
		}
		merged[k] = v
	}
	return merged
}

func isBlankString(v interface{}) bool {
	s, ok := v.(string)
	return ok && strings.TrimSpace(s) == ""
}

// proxyResponseCostHeader carries the USD cost LiteLLM priced the call at. LiteLLM 1.105 sends it on
// non-streamed answers and leaves usage.cost empty for OpenAI and Anthropic, so it is often the only cost.
const proxyResponseCostHeader = "X-Litellm-Response-Cost"

// reportedCost is the cost the answer's usage reports, else the one the proxy's headers carry. A zero or
// unreadable header cost is ignored: a zero may only mean the proxy has no price for the model.
func reportedCost(usageCost *float64, headers http.Header) *float64 {
	if usageCost != nil {
		return usageCost
	}
	cost, err := strconv.ParseFloat(filledHeader(headers, proxyResponseCostHeader), 64)
	if err != nil || math.IsNaN(cost) || math.IsInf(cost, 0) || cost <= 0 {
		return nil
	}
	return &cost
}

func filledHeader(headers http.Header, name string) string {
	v := headers.Get(name)
	if isBlankString(v) {
		return ""
	}
	return v
}
