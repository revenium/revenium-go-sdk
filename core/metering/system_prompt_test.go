package metering

import (
	"strings"
	"testing"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/stretchr/testify/assert"
)

func TestApplyMetadata_SystemPromptIsSentOnlyWhenPromptCaptureIsOn(t *testing.T) {
	t.Setenv(core.EnvCapturePrompts, "")

	off := recordWith(t, OperationChat, map[string]interface{}{"systemPrompt": "You are terse."})
	on := recordWith(t, OperationChat, map[string]interface{}{"systemPrompt": "You are terse.", "capturePrompts": true})

	assert.NotContains(t, off, "systemPrompt")
	assert.Equal(t, "You are terse.", on["systemPrompt"])
}

func TestApplyMetadata_SystemPromptCredentialsAreRedacted(t *testing.T) {
	t.Setenv(core.EnvCapturePrompts, "true")
	key := "sk-" + strings.Repeat("a", 40)

	record := recordWith(t, OperationChat, map[string]interface{}{"systemPrompt": "Use the key " + key + " for lookups."})

	assert.NotContains(t, record["systemPrompt"], key)
	assert.Contains(t, record["systemPrompt"], "REDACTED")
}

func TestApplyMetadata_SystemPromptIsCutToTheMaxPromptSize(t *testing.T) {
	t.Setenv(core.EnvCapturePrompts, "true")
	t.Setenv(core.EnvMaxPromptSize, "100")

	record := recordWith(t, OperationChat, map[string]interface{}{"systemPrompt": strings.Repeat("x", 500)})

	assert.Len(t, []rune(record["systemPrompt"].(string)), 100)
	assert.Equal(t, true, record["promptsTruncated"])
}
