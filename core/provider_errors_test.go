package core

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestProviderErrorKind_ReadsTheErrorTypeAndCodeOfEachProviderShape(t *testing.T) {
	cases := map[string]struct {
		body string
		want string
	}{
		"openai":           {`{"error":{"message":"Incorrect API key provided: sk-proj-****WXYZ","type":"invalid_request_error","param":null,"code":"invalid_api_key"}}`, "invalid_request_error/invalid_api_key"},
		"openai raw error": {`{"message":"slow down","type":"requests","code":"rate_limit_exceeded"}`, "requests/rate_limit_exceeded"},
		"anthropic":        {`{"type":"error","error":{"type":"rate_limit_error","message":"slow down"}}`, "rate_limit_error"},
		"google":           {`{"error":{"code":429,"message":"quota","status":"RESOURCE_EXHAUSTED"}}`, "RESOURCE_EXHAUSTED"},
		"litellm":          {`{"error":{"message":"Authentication Error","type":"auth_error","param":"None","code":"401"}}`, "auth_error"},
		"ollama":           {`{"error":"model 'llama9' not found"}`, ""},
		"not json":         {`upstream connect error`, ""},
		"free text type":   {`{"error":{"type":"say hello please"}}`, ""},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tc.want, ProviderErrorKind([]byte(tc.body)))
		})
	}
}

func TestWithProviderAnswer_KeepsTheStatusAndKindOnTheError(t *testing.T) {
	err := NewProviderError("Groq API returned 429: {...}", nil).
		WithProviderAnswer(429, []byte(`{"error":{"type":"tokens","code":"rate_limit_exceeded"}}`))

	assert.Equal(t, 429, err.StatusCode)
	assert.Equal(t, "tokens/rate_limit_exceeded", err.GetDetails()[DetailProviderErrorKind])
	assert.Equal(t, "[PROVIDER_ERROR] Groq API returned 429: {...}", err.Error(), "the caller still gets the provider's whole answer")
}

func TestErrorKindFrom_SkipsNumbersEnvelopesAndRepeats(t *testing.T) {
	assert.Equal(t, "ThrottlingException", ErrorKindFrom("ThrottlingException", "429", "error", "ThrottlingException"))
	assert.Equal(t, "", ErrorKindFrom("", "Rate limited, retry later"))
}
