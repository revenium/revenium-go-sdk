package prompt

import (
	"encoding/json"
	"os"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/revenium/revenium-go-sdk/core"
)

const (
	DefaultMaxPromptSize = 50000
	truncationSuffix     = "... [truncated]"
	redacted             = "***REDACTED***"
)

type CapturedPrompts struct {
	SystemPrompt string `json:"systemPrompt,omitempty"`
	InputPrompt  string `json:"inputPrompt,omitempty"`
	OutputText   string `json:"outputText,omitempty"`
}

var credentialPatterns = []struct {
	regex       *regexp.Regexp
	replacement string
}{
	{regexp.MustCompile(`pplx-[a-zA-Z0-9_-]{20,}`), "pplx-" + redacted},
	{regexp.MustCompile(`sk-proj-[a-zA-Z0-9_-]{48,}`), "sk-proj-" + redacted},
	{regexp.MustCompile(`sk-ant-[a-zA-Z0-9_-]{20,}`), "sk-ant-" + redacted},
	{regexp.MustCompile(`sk-[a-zA-Z0-9_-]{20,}`), "sk-" + redacted},
	{regexp.MustCompile(`AKIA[A-Z0-9]{16}`), "AKIA" + redacted},
	{regexp.MustCompile(`ghp_[a-zA-Z0-9]{36,}`), "ghp_" + redacted},
	{regexp.MustCompile(`ghs_[a-zA-Z0-9]{36,}`), "ghs_" + redacted},
	{regexp.MustCompile(`eyJ[a-zA-Z0-9_-]+\.eyJ[a-zA-Z0-9_-]+\.[a-zA-Z0-9_-]+`), "***REDACTED_JWT***"},
	{regexp.MustCompile(`(?i)Bearer\s+[a-zA-Z0-9_\-.+/=]{20,}`), "Bearer " + redacted},
	{regexp.MustCompile(`(?i)api[_-]?key["'\s:=]+[a-zA-Z0-9_\-.+/=]{20,}`), "api_key: " + redacted},
	{regexp.MustCompile(`(?i)token["'\s:=]+[a-zA-Z0-9_\-.+/=]{20,}`), "token: " + redacted},
	{regexp.MustCompile(`(?i)password\s*[:=]\s*["']?([^"'\s]{8,})["']?`), "password: " + redacted},
	{regexp.MustCompile(`(?i)secret\s*[:=]\s*["']?([^"'\s]{8,})["']?`), "secret: " + redacted},
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
	return strings.EqualFold(os.Getenv(core.EnvCapturePrompts), "true")
}

func SanitizeCredentials(text string) string {
	if text == "" {
		return text
	}
	for _, p := range credentialPatterns {
		text = p.regex.ReplaceAllString(text, p.replacement)
	}
	return text
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
	suffixLen := utf8.RuneCountInString(truncationSuffix)
	if maxSize <= suffixLen {
		return string([]rune(text)[:maxSize])
	}
	runes := []rune(text)
	return string(runes[:maxSize-suffixLen]) + truncationSuffix
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
