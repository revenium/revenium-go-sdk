package prompt

import (
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/revenium/revenium-go-sdk/core"
)

const (
	DefaultMaxPromptSize = 50000
	truncationSuffix     = "...[TRUNCATED]"
)

type CapturedPrompts struct {
	SystemPrompt string `json:"systemPrompt,omitempty"`
	InputPrompt  string `json:"inputPrompt,omitempty"`
	OutputText   string `json:"outputText,omitempty"`
}

func ShouldCapturePrompts(metadata map[string]interface{}) bool {
	if metadata != nil {
		if val, ok := metadata["capturePrompts"]; ok {
			switch v := val.(type) {
			case bool:
				return v
			case string:
				return strings.EqualFold(v, "true")
			}
		}
	}
	return core.IsEnvFlagSet(core.EnvCapturePrompts)
}

// WithClientCaptureSetting returns a copy of metadata in which a client's
// own prompt-capture setting decides ShouldCapturePrompts, unless the call
// set capturePrompts itself: the call's explicit choice always wins.
func WithClientCaptureSetting(metadata map[string]interface{}, clientCapture bool) map[string]interface{} {
	withSetting := core.CloneMetadata(metadata)
	if withSetting == nil {
		withSetting = map[string]interface{}{}
	}
	if _, set := withSetting["capturePrompts"]; !set {
		withSetting["capturePrompts"] = clientCapture
	}
	return withSetting
}

// CapturesFor reports whether a call's record carries its prompt and output: a capturePrompts
// value in the call's metadata wins over the client's own setting.
func CapturesFor(metadata map[string]interface{}, clientCapture bool) bool {
	return ShouldCapturePrompts(WithClientCaptureSetting(metadata, clientCapture))
}

func GetMaxPromptSize() int {
	if val := os.Getenv(core.EnvMaxPromptSize); val != "" {
		if n, err := strconv.Atoi(val); err == nil && n > 0 {
			return n
		}
	}
	return DefaultMaxPromptSize
}

func TruncateString(text string, maxSize int) string {
	if text == "" || utf8.RuneCountInString(text) <= maxSize {
		return text
	}
	return MarkTruncated(text, maxSize)
}

// MarkTruncated cuts text so that, with the truncation marker appended, it fits in maxSize
// characters; it marks text that was already cut before it got here.
func MarkTruncated(text string, maxSize int) string {
	suffixLen := utf8.RuneCountInString(truncationSuffix)
	runes := []rune(text)
	if maxSize <= suffixLen {
		return string(runes[:min(len(runes), maxSize)])
	}
	return string(runes[:min(len(runes), maxSize-suffixLen)]) + truncationSuffix
}

func ExtractPrompts(messages interface{}, responseContent string, metadata map[string]interface{}) *CapturedPrompts {
	if !ShouldCapturePrompts(metadata) {
		return nil
	}

	maxSize := GetMaxPromptSize()
	captured := &CapturedPrompts{}

	if messages != nil {
		system, input := extractFromMessages(messages)
		if system != "" {
			captured.SystemPrompt = TruncateString(SanitizeCredentials(system), maxSize)
		}
		if input != "" {
			captured.InputPrompt = TruncateString(SanitizeCredentials(input), maxSize)
		}
	}

	if responseContent != "" {
		captured.OutputText = TruncateString(SanitizeCredentials(responseContent), maxSize)
	}

	if captured.SystemPrompt == "" && captured.InputPrompt == "" && captured.OutputText == "" {
		return nil
	}

	return captured
}

func extractFromMessages(messages interface{}) (system, input string) {
	var msgList []interface{}

	switch v := messages.(type) {
	case []interface{}:
		msgList = v
	case []map[string]interface{}:
		for _, m := range v {
			msgList = append(msgList, m)
		}
	case string:
		var parsed []interface{}
		if err := json.Unmarshal([]byte(v), &parsed); err != nil {
			return "", v
		}
		msgList = parsed
	default:
		data, err := json.Marshal(messages)
		if err != nil {
			return "", ""
		}
		if err := json.Unmarshal(data, &msgList); err != nil {
			return "", ""
		}
	}

	var systemParts []string
	var inputParts []string

	for _, msg := range msgList {
		m, ok := msg.(map[string]interface{})
		if !ok {
			continue
		}

		role, _ := m["role"].(string)
		content := extractContent(m)
		if content == "" {
			continue
		}

		switch role {
		case "system":
			systemParts = append(systemParts, content)
		default:
			inputParts = append(inputParts, "["+role+"]\n"+content)
		}
	}

	return strings.Join(systemParts, "\n\n"), strings.Join(inputParts, "\n\n")
}

func extractContent(msg map[string]interface{}) string {
	switch c := msg["content"].(type) {
	case string:
		return c
	case []interface{}:
		return extractContentBlocks(c)
	case []map[string]interface{}:
		blocks := make([]interface{}, len(c))
		for i, b := range c {
			blocks[i] = b
		}
		return extractContentBlocks(blocks)
	default:
		return ""
	}
}

func extractContentBlocks(blocks []interface{}) string {
	var parts []string
	for _, block := range blocks {
		b, ok := block.(map[string]interface{})
		if !ok {
			continue
		}
		switch b["type"] {
		case "text":
			if text, ok := b["text"].(string); ok {
				parts = append(parts, text)
			}
		case "image_url", "image":
			parts = append(parts, "[IMAGE]")
		}
	}
	return strings.Join(parts, "\n")
}
