package litellm

import (
	"net/http"
	"strconv"
	"strings"
)

var headerMetadataFields = map[string]string{
	"X-Revenium-Subscriber-Id":              "subscriberId",
	"X-Revenium-Product-Name":               "productName",
	"X-Revenium-Product-Id":                 "productName",
	"X-Revenium-Organization-Name":          "organizationName",
	"X-Revenium-Organization-Id":            "organizationName",
	"X-Revenium-Trace-Id":                   "traceId",
	"X-Revenium-Task-Type":                  "taskType",
	"X-Revenium-Agent":                      "agent",
	"X-Revenium-Subscriber-Email":           "subscriberEmail",
	"X-Revenium-Subscription-Id":            "subscriptionId",
	"X-Revenium-Subscriber-Credential-Name": "subscriberCredentialName",
	"X-Revenium-Subscriber-Credential":      "subscriberCredential",
	"X-Revenium-Environment":                "environment",
	"X-Revenium-Operation-Subtype":          "operationSubtype",
	"X-Revenium-Parent-Transaction-Id":      "parentTransactionId",
	"X-Revenium-Transaction-Name":           "transactionName",
	"X-Revenium-Region":                     "region",
	"X-Revenium-Credential-Alias":           "credentialAlias",
	"X-Revenium-Trace-Type":                 "traceType",
	"X-Revenium-Trace-Name":                 "traceName",
}

// ExtractMetadataFromHeaders maps Revenium-prefixed HTTP headers to the usage metadata shape
func ExtractMetadataFromHeaders(headers http.Header) map[string]interface{} {
	if headers == nil {
		return nil
	}
	out := map[string]interface{}{}
	for header, field := range headerMetadataFields {
		if v := headers.Get(header); v != "" {
			out[field] = v
		}
	}
	if v := headers.Get("X-Revenium-Retry-Number"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			out["retryNumber"] = n
		}
	}
	if v := headers.Get("X-Revenium-Capture-Prompts"); v != "" {
		out["capturePrompts"] = strings.EqualFold(v, "true")
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// MergeMetadata merges header-derived metadata under context-derived metadata
// context values take precedence; nil context is treated as empty
func MergeMetadata(contextMeta, headerMeta map[string]interface{}) map[string]interface{} {
	if len(contextMeta) == 0 && len(headerMeta) == 0 {
		return nil
	}
	merged := make(map[string]interface{}, len(contextMeta)+len(headerMeta))
	for k, v := range headerMeta {
		merged[k] = v
	}
	for k, v := range contextMeta {
		merged[k] = v
	}
	return merged
}
