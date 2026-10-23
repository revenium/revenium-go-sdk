package metering

import (
	"math"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func recordWith(t *testing.T, op OperationType, metadata map[string]interface{}) map[string]interface{} {
	t.Helper()
	p := NewPayload(op, "model", "OPENAI").Build()
	ApplyMetadata(p, metadata)
	return wireFields(t, p)
}

func TestApplyMetadata_AttributionAndPricingFieldsReachTheCompletionRecord(t *testing.T) {
	record := recordWith(t, OperationChat, map[string]interface{}{
		"agent":                "support-bot",
		"agentVersion":         "2.3.1",
		"squadId":              "squad-7",
		"squadName":            "billing",
		"squadRole":            "triage",
		"transactionName":      "refund-flow",
		"pricingTier":          "BATCH",
		"billingSkipped":       true,
		"costMultiplier":       0.5,
		"requestedServiceTier": "priority",
		"actualServiceTier":    "default",
		"effort":               "high",
		"speed":                "fast",
		"systemPrompt":         "You are terse.",
		"capturePrompts":       true,
	})

	assert.Equal(t, map[string]interface{}{
		"agent":                "support-bot",
		"agentVersion":         "2.3.1",
		"squadId":              "squad-7",
		"squadName":            "billing",
		"squadRole":            "triage",
		"transactionName":      "refund-flow",
		"pricingTier":          "BATCH",
		"billingSkipped":       true,
		"costMultiplier":       0.5,
		"requestedServiceTier": "priority",
		"actualServiceTier":    "default",
		"effort":               "high",
		"speed":                "fast",
		"systemPrompt":         "You are terse.",
	}, pick(record, "agent", "agentVersion", "squadId", "squadName", "squadRole", "transactionName", "pricingTier",
		"billingSkipped", "costMultiplier", "requestedServiceTier", "actualServiceTier", "effort", "speed", "systemPrompt"))
}

func TestApplyMetadata_PromptContextAndSkillFieldsReachTheCompletionRecord(t *testing.T) {
	fields := map[string]interface{}{
		"querySource":                "repl_main_thread:outputStyle:custom",
		"subagentType":               "general-purpose",
		"promptId":                   "0f2b8a54-6c31",
		"clientSurface":              "claude-code",
		"subscriptionTier":           "pro",
		"codingAssistantAccountUuid": "123e4567-e89b-12d3-a456-426614174000",
		"skillName":                  "pdf",
		"skillInvocationTrigger":     "auto",
		"skillSource":                "plugin",
		"skillKind":                  "skill",
		"skillPluginName":            "docs",
		"skillMarketplaceName":       "official",
	}
	metadata := map[string]interface{}{"promptLength": 412}
	for key, value := range fields {
		metadata[key] = value
	}

	record := recordWith(t, OperationChat, metadata)

	for key, value := range fields {
		assert.Equal(t, value, record[key], key)
	}
	assert.Equal(t, float64(412), record["promptLength"])
}

func TestApplyMetadata_SourceTransactionIDAndPriorityTierReachImageAndVideoRecords(t *testing.T) {
	for _, op := range []OperationType{OperationImage, OperationVideo} {
		record := recordWith(t, op, map[string]interface{}{"sourceTransactionId": "tx-source", "priorityTier": "priority"})

		assert.Equal(t, "tx-source", record["sourceTransactionId"], op)
		assert.Equal(t, "priority", record["priorityTier"], op)
	}
}

func TestApplyMetadata_SharedPricingFieldsReachEveryMediaRecord(t *testing.T) {
	for _, op := range []OperationType{OperationImage, OperationAudio, OperationVideo} {
		record := recordWith(t, op, map[string]interface{}{
			"agentVersion": "2.3.1", "squadName": "billing", "pricingTier": "batch", "billingSkipped": "true",
			"requestedServiceTier": "flex", "actualServiceTier": "flex",
		})

		assert.Equal(t, map[string]interface{}{
			"agentVersion": "2.3.1", "squadName": "billing", "pricingTier": "BATCH", "billingSkipped": true,
			"requestedServiceTier": "flex", "actualServiceTier": "flex",
		}, pick(record, "agentVersion", "squadName", "pricingTier", "billingSkipped", "requestedServiceTier", "actualServiceTier"), op)
	}
}

func TestApplyMetadata_CompletionOnlyFieldsStayOffMediaRecords(t *testing.T) {
	for _, op := range []OperationType{OperationImage, OperationAudio, OperationVideo} {
		record := recordWith(t, op, map[string]interface{}{"costMultiplier": 0.5, "effort": "high", "systemPrompt": "x", "speed": "fast"})

		assert.Empty(t, pick(record, "costMultiplier", "effort", "systemPrompt", "speed"), op)
	}
}

func TestApplyMetadata_AudioSpeedStaysTheTextToSpeechRate(t *testing.T) {
	rate := 1.25
	p := NewPayload(OperationAudio, "tts-1", "OPENAI").WithAudioDetails(AudioDetails{Speed: &rate}).Build()
	ApplyMetadata(p, map[string]interface{}{"speed": "fast"})

	assert.Equal(t, 1.25, wireFields(t, p)["speed"])
}

func TestApplyMetadata_ValuesThePlatformWouldRefuseAreLeftOff(t *testing.T) {
	logger := useRecordingLogger(t)
	record := recordWith(t, OperationChat, map[string]interface{}{
		"pricingTier":    "FREE",
		"effort":         "very high",
		"speed":          strings.Repeat("f", 17),
		"promptLength":   -1,
		"costMultiplier": math.Inf(1),
		"billingSkipped": "sometimes",
		"agentVersion":   42,
		"model":          "kept-out",
	})

	assert.Empty(t, pick(record, "pricingTier", "effort", "speed", "promptLength", "costMultiplier", "billingSkipped", "agentVersion"))
	warnings := strings.Join(logger.Lines(), "\n")
	for _, key := range []string{"pricingTier", "effort", "speed", "promptLength", "costMultiplier", "billingSkipped", "agentVersion"} {
		assert.Contains(t, warnings, "leaving "+key+" off the usage record", key)
	}
}

func TestApplyMetadata_AgentVersionIsCutToSixtyFourCharacters(t *testing.T) {
	record := recordWith(t, OperationChat, map[string]interface{}{"agentVersion": strings.Repeat("v", 70)})

	assert.Equal(t, strings.Repeat("v", 64), record["agentVersion"])
}

func TestApplyMetadata_BlankValuesCountAsUnset(t *testing.T) {
	record := recordWith(t, OperationChat, map[string]interface{}{
		"organizationName": "  ",
		"productName":      "\t",
		"agent":            " ",
		"ticketId":         "  ",
		"traceId":          "\n",
		"errorReason":      "  ",
		"squadName":        "   ",
		"pricingTier":      " ",
	})

	assert.Empty(t, pick(record, "organizationName", "productName", "agent", "ticketId", "traceId", "errorReason", "squadName", "pricingTier"))
	assert.Equal(t, "END", record["stopReason"])
}

func TestApplyMetadata_FilledValuesAreSentAsReceived(t *testing.T) {
	record := recordWith(t, OperationChat, map[string]interface{}{"organizationName": " Acme ", "squadName": " billing "})

	assert.Equal(t, " Acme ", record["organizationName"])
	assert.Equal(t, " billing ", record["squadName"])
}

func forgetWarnedMetadataKeys(t *testing.T) {
	t.Helper()
	clear := func() {
		warnedMetadataKeysMu.Lock()
		defer warnedMetadataKeysMu.Unlock()
		warnedMetadataKeys = map[string]struct{}{}
	}
	clear()
	t.Cleanup(clear)
}

func TestApplyMetadata_WarnsOnceAboutAKeyItDoesNotRecord(t *testing.T) {
	forgetWarnedMetadataKeys(t)
	logger := useRecordingLogger(t)
	metadata := map[string]interface{}{"agentVerison": "2.3.1", "agent": "support-bot", "model": "gpt-4.1-nano"}

	recordWith(t, OperationChat, metadata)
	recordWith(t, OperationChat, metadata)

	var warnings []string
	for _, line := range logger.Lines() {
		if strings.Contains(line, "is not a field Revenium records") {
			warnings = append(warnings, line)
		}
	}
	require.Len(t, warnings, 1)
	assert.Contains(t, warnings[0], `"agentVerison"`)
}

func pick(record map[string]interface{}, keys ...string) map[string]interface{} {
	picked := map[string]interface{}{}
	for _, key := range keys {
		if value, ok := record[key]; ok {
			picked[key] = value
		}
	}
	return picked
}

func TestMarshal_AnAudioRateWinsOverASpeedMode(t *testing.T) {
	rate := 1.25
	p := NewPayload(OperationAudio, "tts-1", "OPENAI").Build()
	p.Speed = &rate
	p.SpeedMode = "fast"

	assert.Equal(t, 1.25, wireFields(t, p)["speed"])
}

func TestMarshal_ACompletionSpeedModeIsSentAsSpeed(t *testing.T) {
	p := NewPayload(OperationChat, "claude-haiku-4-5", "ANTHROPIC").Build()
	p.SpeedMode = "fast"

	assert.Equal(t, "fast", wireFields(t, p)["speed"])
}
