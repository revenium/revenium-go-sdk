package metering

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func capturedContent(t *testing.T, payload *MeteringPayload) string {
	t.Helper()
	var messages []map[string]string
	require.NoError(t, json.Unmarshal([]byte(payload.InputMessages), &messages))
	require.Len(t, messages, 1)
	return messages[0]["content"]
}

func TestApplyCapturedPrompt_RedactsCredentialsInPromptAndAnswer(t *testing.T) {
	payload := &MeteringPayload{}

	ApplyCapturedPrompt(payload, "use api_key=sk-live-SECRETPROMPT1234567890ab here", "token: sk-live-SECRETANSWER1234567890ab")

	assert.NotContains(t, capturedContent(t, payload), "SECRETPROMPT")
	assert.NotContains(t, payload.OutputResponse, "SECRETANSWER")
	assert.False(t, payload.PromptsTruncated)
}

func TestApplyCapturedPrompt_CutsPromptAndAnswerToTheConfiguredLimit(t *testing.T) {
	t.Setenv(core.EnvMaxPromptSize, "1000")
	payload := &MeteringPayload{}

	ApplyCapturedPrompt(payload, strings.Repeat("p", 60054), strings.Repeat("a", 5000))

	assert.LessOrEqual(t, utf8.RuneCountInString(capturedContent(t, payload)), 1000)
	assert.LessOrEqual(t, utf8.RuneCountInString(payload.OutputResponse), 1000)
	assert.True(t, payload.PromptsTruncated)
}

func TestApplyCapturedPrompt_MarksTruncationWhenOnlyTheAnswerWasCut(t *testing.T) {
	t.Setenv(core.EnvMaxPromptSize, "100")
	payload := &MeteringPayload{}

	ApplyCapturedPrompt(payload, "short", strings.Repeat("a", 500))

	assert.True(t, payload.PromptsTruncated)
}

func TestApplyCapturedPrompt_LeavesEmptyValuesUnset(t *testing.T) {
	payload := &MeteringPayload{}

	ApplyCapturedPrompt(payload, "", "")

	assert.Empty(t, payload.InputMessages)
	assert.Empty(t, payload.OutputResponse)
	assert.False(t, payload.PromptsTruncated)
}

func builderCapture(input, output string, truncated bool) *MeteringPayload {
	return NewPayload(OperationChat, "gpt-4o", "OpenAI").WithPromptCapture(input, output, truncated).Build()
}

func TestWithPromptCapture_StoresNothingWhenCaptureIsOff(t *testing.T) {
	t.Setenv(core.EnvCapturePrompts, "false")

	payload := builderCapture(`[{"role":"user","content":"hello"}]`, "hi there", true)

	assert.Empty(t, payload.InputMessages)
	assert.Empty(t, payload.OutputResponse)
	assert.False(t, payload.PromptsTruncated)
}

func TestWithPromptCapture_RedactsCredentialsInMessagesAndReply(t *testing.T) {
	t.Setenv(core.EnvCapturePrompts, "true")

	payload := builderCapture(`[{"role":"user","content":"use api_key=sk-live-SECRETPROMPT1234567890ab here"}]`, "token: sk-live-SECRETANSWER1234567890ab", false)

	assert.NotEmpty(t, payload.InputMessages)
	assert.NotContains(t, payload.InputMessages, "SECRETPROMPT")
	assert.NotEmpty(t, payload.OutputResponse)
	assert.NotContains(t, payload.OutputResponse, "SECRETANSWER")
	assert.False(t, payload.PromptsTruncated)
}

func TestWithPromptCapture_StoresPlainPromptTextAsAUserMessage(t *testing.T) {
	t.Setenv(core.EnvCapturePrompts, "true")

	payload := builderCapture("password=hunter2-SECRET what is the weather", "", false)

	var messages []map[string]string
	require.NoError(t, json.Unmarshal([]byte(payload.InputMessages), &messages))
	require.Len(t, messages, 1)
	assert.Equal(t, "user", messages[0]["role"])
	assert.Contains(t, messages[0]["content"], "what is the weather")
	assert.NotContains(t, messages[0]["content"], "hunter2-SECRET")
}

func TestWithPromptCapture_CutsEachFieldToTheConfiguredLimit(t *testing.T) {
	t.Setenv(core.EnvCapturePrompts, "true")
	t.Setenv(core.EnvMaxPromptSize, "1000")
	messages, err := json.Marshal([]map[string]string{{"role": "user", "content": strings.Repeat("p", 60000)}})
	require.NoError(t, err)

	payload := builderCapture(string(messages), strings.Repeat("a", 5000), false)

	assert.LessOrEqual(t, utf8.RuneCountInString(payload.InputMessages), 1000)
	assert.True(t, json.Valid([]byte(payload.InputMessages)), "a cut message list stays valid JSON")
	assert.LessOrEqual(t, utf8.RuneCountInString(payload.OutputResponse), 1000)
	assert.True(t, payload.PromptsTruncated)
}

func TestWithPromptCapture_KeepsTheCallersTruncationMark(t *testing.T) {
	t.Setenv(core.EnvCapturePrompts, "true")

	payload := builderCapture(`[{"role":"user","content":"hello"}]`, "partial reply", true)

	assert.True(t, payload.PromptsTruncated)
}
