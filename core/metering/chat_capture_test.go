package metering

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/revenium/revenium-go-sdk/core/prompt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func sampleChat() ChatCapture {
	return ChatCapture{
		SystemPrompt: "You are terse.",
		Messages:     []interface{}{map[string]interface{}{"role": "user", "content": "What is 2+2?"}},
		Output:       "4",
	}
}

func TestApplyChatCapture_RecordsNothingWhenCaptureIsOff(t *testing.T) {
	t.Setenv(core.EnvCapturePrompts, "")
	payload := &MeteringPayload{}
	called := false

	ApplyChatCapture(payload, nil, func() ChatCapture { called = true; return sampleChat() })

	assert.False(t, called)
	assert.Empty(t, payload.SystemPrompt)
	assert.Empty(t, payload.InputMessages)
	assert.Empty(t, payload.OutputResponse)
	assert.False(t, payload.PromptsTruncated)
}

func TestApplyChatCapture_RecordsPromptAndReplyWhenCaptureIsOn(t *testing.T) {
	t.Setenv(core.EnvCapturePrompts, "true")
	payload := &MeteringPayload{}

	ApplyChatCapture(payload, nil, sampleChat)

	assert.Equal(t, "You are terse.", payload.SystemPrompt)
	assert.JSONEq(t, `[{"role":"user","content":"What is 2+2?"}]`, payload.InputMessages)
	assert.Equal(t, "4", payload.OutputResponse)
	assert.False(t, payload.PromptsTruncated)
}

func TestApplyChatCapture_FollowsThePerCallSetting(t *testing.T) {
	t.Setenv(core.EnvCapturePrompts, "true")
	off := &MeteringPayload{}
	ApplyChatCapture(off, map[string]interface{}{"capturePrompts": false}, sampleChat)
	assert.Empty(t, off.InputMessages)

	t.Setenv(core.EnvCapturePrompts, "false")
	on := &MeteringPayload{}
	ApplyChatCapture(on, map[string]interface{}{"capturePrompts": true}, sampleChat)
	assert.NotEmpty(t, on.InputMessages)
}

func TestApplyChatCapture_RedactsCredentialsInEveryField(t *testing.T) {
	t.Setenv(core.EnvCapturePrompts, "true")
	payload := &MeteringPayload{}

	ApplyChatCapture(payload, nil, func() ChatCapture {
		return ChatCapture{
			SystemPrompt: "use sk-ant-SYSTEMSECRET1234567890ab",
			Messages:     []interface{}{map[string]interface{}{"role": "user", "content": "key sk-ant-INPUTSECRET1234567890abc"}},
			Output:       "Bearer OUTPUTSECRET1234567890abcdef",
		}
	})

	assert.NotContains(t, payload.SystemPrompt, "SYSTEMSECRET")
	assert.NotContains(t, payload.InputMessages, "INPUTSECRET")
	assert.NotContains(t, payload.OutputResponse, "OUTPUTSECRET")
}

func TestApplyChatCapture_CutsEachFieldToTheLimitAndFlagsIt(t *testing.T) {
	t.Setenv(core.EnvCapturePrompts, "true")
	t.Setenv(core.EnvMaxPromptSize, "300")
	payload := &MeteringPayload{}

	ApplyChatCapture(payload, nil, func() ChatCapture {
		return ChatCapture{
			SystemPrompt: strings.Repeat("s", 1000),
			Messages:     []interface{}{map[string]interface{}{"role": "user", "content": strings.Repeat("i", 1000)}},
			Output:       strings.Repeat("o", 1000),
		}
	})

	assert.LessOrEqual(t, utf8.RuneCountInString(payload.SystemPrompt), 300)
	assert.LessOrEqual(t, utf8.RuneCountInString(payload.InputMessages), 300)
	assert.LessOrEqual(t, utf8.RuneCountInString(payload.OutputResponse), 300)
	assert.True(t, strings.HasSuffix(payload.OutputResponse, "...[TRUNCATED]"))
	assert.True(t, json.Valid([]byte(payload.InputMessages)))
	assert.True(t, payload.PromptsTruncated)
}

func TestApplyChatCapture_RequestSystemPromptReplacesTheMetadataOne(t *testing.T) {
	t.Setenv(core.EnvCapturePrompts, "true")
	metadata := map[string]interface{}{"systemPrompt": "from metadata"}
	payload := &MeteringPayload{OperationType: string(OperationChat)}
	ApplyMetadata(payload, metadata)

	ApplyChatCapture(payload, metadata, sampleChat)

	assert.Equal(t, "You are terse.", payload.SystemPrompt)
}

func TestApplyChatCapture_DoesNotInheritTheCutOfAReplacedMetadataSystemPrompt(t *testing.T) {
	t.Setenv(core.EnvCapturePrompts, "true")
	t.Setenv(core.EnvMaxPromptSize, "300")
	metadata := map[string]interface{}{"systemPrompt": strings.Repeat("m", 1000)}
	payload := &MeteringPayload{OperationType: string(OperationChat)}
	ApplyMetadata(payload, metadata)
	require.True(t, payload.PromptsTruncated)

	ApplyChatCapture(payload, metadata, sampleChat)

	assert.False(t, payload.PromptsTruncated)
}

func TestApplyChatCapture_FlagsAStreamedReplyTheCollectorCutEvenWhenRedactionShrinksIt(t *testing.T) {
	t.Setenv(core.EnvCapturePrompts, "true")
	t.Setenv(core.EnvMaxPromptSize, "300")
	collector := prompt.NewReplyCollector()
	collector.Add(strings.Repeat("sk-ant-"+strings.Repeat("k", 1000)+" ", 6))
	collector.End()
	require.True(t, collector.Truncated())
	payload := &MeteringPayload{}

	ApplyChatCapture(payload, nil, func() ChatCapture { return ChatCapture{}.WithStreamedReply(collector) })

	assert.True(t, payload.PromptsTruncated)
	assert.True(t, strings.HasSuffix(payload.OutputResponse, "...[TRUNCATED]"))
	assert.NotContains(t, payload.OutputResponse, "kkkkkkkkkk")
	assert.LessOrEqual(t, utf8.RuneCountInString(payload.OutputResponse), 300)
}

func TestApplyChatCapture_RedactsACredentialThatCrossesTheLimit(t *testing.T) {
	t.Setenv(core.EnvCapturePrompts, "true")
	t.Setenv(core.EnvMaxPromptSize, "100")
	collector := prompt.NewReplyCollector()
	collector.Add(strings.Repeat(".", 70) + "ghp_" + strings.Repeat("a", 40))
	collector.End()
	payload := &MeteringPayload{}

	ApplyChatCapture(payload, nil, func() ChatCapture { return ChatCapture{}.WithStreamedReply(collector) })

	assert.Equal(t, strings.Repeat(".", 70)+"ghp_***REDACTED***", payload.OutputResponse)
}

func TestApplyChatCapture_RedactsABasicCredentialTheStreamedReplyBudgetCut(t *testing.T) {
	t.Setenv(core.EnvCapturePrompts, "true")
	t.Setenv(core.EnvMaxPromptSize, "300")
	credential := base64.StdEncoding.EncodeToString([]byte("admin:" + strings.Repeat("p", 5000)))
	collector := prompt.NewReplyCollector()
	for reply := "Basic " + credential; reply != ""; {
		chunk := reply[:min(len(reply), 64)]
		collector.Add(chunk)
		reply = reply[len(chunk):]
	}
	collector.End()
	require.True(t, collector.Truncated())
	payload := &MeteringPayload{}

	ApplyChatCapture(payload, nil, func() ChatCapture { return ChatCapture{}.WithStreamedReply(collector) })

	assert.Equal(t, "Basic ***REDACTED***...[TRUNCATED]", payload.OutputResponse)
	assert.NotContains(t, payload.OutputResponse, credential[:16])
	assert.True(t, payload.PromptsTruncated)
}

func TestApplyChatCapture_KeepsTheMetadataSystemPromptWhenTheRequestHasNone(t *testing.T) {
	t.Setenv(core.EnvCapturePrompts, "true")
	metadata := map[string]interface{}{"systemPrompt": "from metadata"}
	payload := &MeteringPayload{OperationType: string(OperationChat)}
	ApplyMetadata(payload, metadata)

	ApplyChatCapture(payload, metadata, func() ChatCapture { return ChatCapture{Output: "4"} })

	assert.Equal(t, "from metadata", payload.SystemPrompt)
}

func TestApplyChatCapture_KeepsTheRecordWhenCaptureFails(t *testing.T) {
	t.Setenv(core.EnvCapturePrompts, "true")
	payload := &MeteringPayload{Model: "m"}

	ApplyChatCapture(payload, nil, func() ChatCapture { panic("boom") })

	assert.Equal(t, "m", payload.Model)
	assert.Empty(t, payload.InputMessages)
}

func TestChatMessages_ReturnsTheValuesAsEncoded(t *testing.T) {
	type typedMessage struct {
		Role    string `json:"role"`
		Content string `json:"content,omitempty"`
	}

	messages := ChatMessages([]typedMessage{{Role: "user", Content: "hi"}, {Role: "assistant"}})

	assert.Equal(t, []interface{}{
		map[string]interface{}{"role": "user", "content": "hi"},
		map[string]interface{}{"role": "assistant"},
	}, messages)
}

func TestSplitSystemMessage_TakesTheFirstSystemMessageOut(t *testing.T) {
	messages := []interface{}{
		map[string]interface{}{"role": "system", "content": "be brief"},
		map[string]interface{}{"role": "user", "content": "hi"},
		map[string]interface{}{"role": "system", "content": "later"},
		map[string]interface{}{"role": "developer", "content": "dev note"},
	}

	system, others := SplitSystemMessage(messages, false, "system")

	assert.Equal(t, "be brief", system)
	assert.Equal(t, []interface{}{messages[1], messages[3]}, others)
}

func TestReplyCollectorFor_CollectsOnlyWhenCaptureIsOn(t *testing.T) {
	t.Setenv(core.EnvCapturePrompts, "")
	assert.Nil(t, ReplyCollectorFor(nil))

	collector := ReplyCollectorFor(map[string]interface{}{"capturePrompts": true})
	collector.Add("streamed")
	assert.Equal(t, "streamed", collector.Text())
}

func TestOpenAIChatCapture_SplitsTheSystemMessageFromTypedMessages(t *testing.T) {
	type typedMessage struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}

	captured := OpenAIChatCapture([]typedMessage{{"system", "be brief"}, {"user", "hi"}}, "hello")

	assert.Equal(t, ChatCapture{
		SystemPrompt: "be brief",
		Messages:     []interface{}{map[string]interface{}{"role": "user", "content": "hi"}},
		Output:       "hello",
	}, captured)
}

func TestApplyChatCapture_RedactsTheRequestAndMetadataSystemPromptsAlike(t *testing.T) {
	t.Setenv(core.EnvCapturePrompts, "true")
	secretJSON := `{"db_password": "hunter2hunter2"}`
	fromMetadata := &MeteringPayload{OperationType: string(OperationChat)}
	ApplyMetadata(fromMetadata, map[string]interface{}{"systemPrompt": secretJSON})
	fromRequest := &MeteringPayload{OperationType: string(OperationChat)}

	ApplyChatCapture(fromRequest, nil, func() ChatCapture { return ChatCapture{SystemPrompt: secretJSON} })

	assert.NotContains(t, fromMetadata.SystemPrompt, "hunter2hunter2")
	assert.Equal(t, fromMetadata.SystemPrompt, fromRequest.SystemPrompt)
}

func TestApplyChatCapture_FlagsAReplyThatWasDroppedEntirely(t *testing.T) {
	t.Setenv(core.EnvCapturePrompts, "true")
	payload := &MeteringPayload{}

	ApplyChatCapture(payload, nil, func() ChatCapture { return ChatCapture{OutputCut: true} })

	assert.Empty(t, payload.OutputResponse)
	assert.True(t, payload.PromptsTruncated)
}

func TestSplitSystemMessage_KeepsLaterInstructionsWhenAsked(t *testing.T) {
	messages := []interface{}{
		map[string]interface{}{"role": "developer", "content": "be brief"},
		map[string]interface{}{"role": "user", "content": "hi"},
		map[string]interface{}{"role": "system", "content": "later"},
	}

	system, others := SplitSystemMessage(messages, true, "system", "developer")

	assert.Equal(t, "be brief", system)
	assert.Equal(t, []interface{}{messages[1], messages[2]}, others)
}

func TestApplyChatCapture_MarksAStreamThatWasStoppedBeforeItsEnd(t *testing.T) {
	t.Setenv(core.EnvCapturePrompts, "true")
	collector := prompt.NewReplyCollector()
	collector.Add("Par")
	payload := &MeteringPayload{}

	ApplyChatCapture(payload, nil, func() ChatCapture { return ChatCapture{}.WithStreamedReply(collector) })

	assert.Equal(t, "Par...[TRUNCATED]", payload.OutputResponse)
	assert.True(t, payload.PromptsTruncated)
}
