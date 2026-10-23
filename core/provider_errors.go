package core

import (
	"encoding/json"
	"regexp"
	"slices"
	"strings"
)

// DetailProviderErrorKind is the ReveniumError detail holding the error type
// or code the provider put in its reply, such as "rate_limit_error".
const DetailProviderErrorKind = "providerErrorKind"

// DetailProviderReply marks a ReveniumError whose text quotes the provider's
// own reply, which can repeat the request.
const DetailProviderReply = "providerReply"

// The kind is sent to Revenium even when error text is not, so only a short
// identifier qualifies: free text in these fields can quote the request.
var errorKindPattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_.:-]{0,63}$`)

const maxErrorKindParts = 2

// WithProviderAnswer records the HTTP status the provider answered with and
// the error type or code in its reply body.
func (e *ReveniumError) WithProviderAnswer(statusCode int, body []byte) *ReveniumError {
	e.StatusCode = statusCode
	return e.WithProviderReply(body)
}

// WithProviderReply marks the error's text as the provider's own reply, such
// as an error event sent mid-stream, and records its error type or code.
func (e *ReveniumError) WithProviderReply(body []byte) *ReveniumError {
	e.WithDetails(DetailProviderReply, true)
	if kind := ProviderErrorKind(body); kind != "" {
		e.WithDetails(DetailProviderErrorKind, kind)
	}
	return e
}

// ProviderErrorKind reads the error type and code from a provider's JSON
// error reply, in the shapes OpenAI, Anthropic, Google, LiteLLM and Runway
// use, such as "invalid_request_error/invalid_api_key". It is empty when the
// reply names neither as a short identifier.
func ProviderErrorKind(body []byte) string {
	var reply map[string]interface{}
	if json.Unmarshal(body, &reply) != nil {
		return ""
	}
	if nested, ok := reply["error"].(map[string]interface{}); ok {
		if kind := errorKindIn(nested); kind != "" {
			return kind
		}
	}
	return errorKindIn(reply)
}

func errorKindIn(fields map[string]interface{}) string {
	candidates := make([]string, 0, 3)
	for _, name := range []string{"type", "code", "status"} {
		if value, ok := fields[name].(string); ok {
			candidates = append(candidates, value)
		}
	}
	return ErrorKindFrom(candidates...)
}

// ErrorKindFrom joins the first distinct candidates that are short
// identifiers. Anthropic's envelope type "error" and numeric codes, which
// repeat the HTTP status, say nothing and are skipped.
func ErrorKindFrom(candidates ...string) string {
	var parts []string
	for _, candidate := range candidates {
		if len(parts) == maxErrorKindParts {
			break
		}
		if !errorKindPattern.MatchString(candidate) || strings.EqualFold(candidate, "error") || slices.Contains(parts, candidate) {
			continue
		}
		parts = append(parts, candidate)
	}
	return strings.Join(parts, "/")
}
