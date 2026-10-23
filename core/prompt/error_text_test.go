package prompt

import (
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
)

func TestRedactErrorText_RemovesKeyFragmentsHashesAndPersonalData(t *testing.T) {
	cases := map[string]struct {
		text      string
		sensitive []string
	}{
		"masked openai key":    {`Incorrect API key provided: sk-proj-********************FAKEWXYZ. You can find your API key`, []string{"FAKEWXYZ", "sk-proj"}},
		"litellm key and hash": {`Received API Key = sk-...FAKE1234, Key Hash (Token) =0f1e2d3c4b5a69788796a5b4c3d2e1f00f1e2d3c4b5a69788796a5b4c3d2e1f0. Unable`, []string{"FAKE1234", "0f1e2d3c4b5a6978"}},
		"url user info":        {`Post "https://admin:hunter2@proxy.internal/v1/chat": dial tcp`, []string{"admin", "hunter2"}},
		"query string key":     {`Get "https://api.example.com/v1/models?key=AIzaFAKE123&alt=json": 403`, []string{"AIzaFAKE123"}},
		"bearer token":         {`upstream rejected Authorization: Bearer abc.def.ghi`, []string{"abc.def.ghi"}},
		"json secret field":    {`{"api_key": "plain-secret-value", "ok": true}`, []string{"plain-secret-value"}},
		"email":                {`subscriber jane.doe@example.com is over quota`, []string{"jane.doe@example.com"}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			redacted := RedactErrorText(tc.text, 2048)
			for _, part := range tc.sensitive {
				assert.NotContains(t, redacted, part)
			}
			assert.Contains(t, redacted, "[REDACTED]")
		})
	}
}

func TestRedactErrorText_KeepsOrdinaryText(t *testing.T) {
	text := "rate_limit_error: the model is overloaded, retry in 20s (risk-free)"
	assert.Equal(t, text, RedactErrorText(text, 2048))
}

func TestRedactErrorText_CutsToTheLimitAfterRedacting(t *testing.T) {
	redacted := RedactErrorText(strings.Repeat("é", 300)+" sk-proj-"+strings.Repeat("A", 60), 255)
	assert.Equal(t, 255, utf8.RuneCountInString(redacted))
	assert.True(t, utf8.ValidString(redacted))

	assert.Equal(t, "failed [REDACTED]", RedactErrorText("failed sk-ant-"+strings.Repeat("x", 40), 17), "a cut never leaves part of a secret behind")
}

func TestRedactedError_RedactsTheTextOfAnErrorForALogLine(t *testing.T) {
	err := errors.New(`Post "https://svc:pa55word@bedrock.internal/model/invoke?X-Amz-Signature=abc123": EOF`)
	logged := RedactedError(err)

	assert.NotContains(t, logged, "pa55word")
	assert.NotContains(t, logged, "abc123")
	assert.Contains(t, logged, "bedrock.internal/model/invoke")
}

func TestCapturesFor_TheCallsExplicitChoiceWinsOverTheClients(t *testing.T) {
	t.Setenv("REVENIUM_CAPTURE_PROMPTS", "true")
	assert.False(t, CapturesFor(map[string]interface{}{"capturePrompts": false}, true), "per-call false beats client true")
	assert.True(t, CapturesFor(map[string]interface{}{"capturePrompts": true}, false), "per-call true beats client false")
	assert.True(t, CapturesFor(map[string]interface{}{"capturePrompts": "TRUE"}, false), "a per-call string is read as ShouldCapturePrompts reads it")
	assert.False(t, CapturesFor(map[string]interface{}{"agent": "a"}, false), "unset uses the client setting, not the environment")
	assert.True(t, CapturesFor(nil, true))
}

func TestWithClientCaptureSetting_TheCallsExplicitChoiceWins(t *testing.T) {
	assert.False(t, ShouldCapturePrompts(WithClientCaptureSetting(map[string]interface{}{"capturePrompts": false}, true)), "per-call false beats client true")
	assert.True(t, ShouldCapturePrompts(WithClientCaptureSetting(map[string]interface{}{"capturePrompts": true}, false)), "per-call true beats client false")
	assert.True(t, ShouldCapturePrompts(WithClientCaptureSetting(map[string]interface{}{"agent": "a"}, true)), "unset uses the client setting")
	assert.False(t, ShouldCapturePrompts(WithClientCaptureSetting(nil, false)))

	callerMetadata := map[string]interface{}{"agent": "a"}
	WithClientCaptureSetting(callerMetadata, true)
	assert.NotContains(t, callerMetadata, "capturePrompts", "the caller's map is left untouched")
}
