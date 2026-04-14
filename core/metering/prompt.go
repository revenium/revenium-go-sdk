package metering

import (
	"encoding/json"
	"unicode/utf8"

	"github.com/revenium/revenium-go-sdk/core"
)

const MaxPromptLength = 50000
const truncationSuffix = "...[TRUNCATED]"

func FormatPromptAsInputMessages(prompt string) (string, bool) {
	if prompt == "" {
		return "", false
	}

	truncated := false
	runeCount := utf8.RuneCountInString(prompt)
	if runeCount > MaxPromptLength {
		truncateAt := MaxPromptLength - utf8.RuneCountInString(truncationSuffix)
		runes := []rune(prompt)
		prompt = string(runes[:truncateAt]) + truncationSuffix
		truncated = true
	}

	messages := []map[string]string{
		{"role": "user", "content": prompt},
	}

	jsonBytes, err := json.Marshal(messages)
	if err != nil {
		core.Warn("failed to serialize prompt as inputMessages: %v", err)
		return "", truncated
	}

	return string(jsonBytes), truncated
}
