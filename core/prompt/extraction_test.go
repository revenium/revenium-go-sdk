package prompt

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSanitizeCredentials_AllPatterns(t *testing.T) {
	tests := []struct {
		name  string
		input string
		check string
	}{
		{"perplexity key", "key: pplx-abcdefghij1234567890extra", "pplx-***REDACTED***"},
		{"openai project key", "sk-proj-" + strings.Repeat("a", 48), "sk-proj-***REDACTED***"},
		{"anthropic key", "sk-ant-abcdefghij1234567890extra", "sk-ant-***REDACTED***"},
		{"generic sk key", "sk-abcdefghij1234567890extra", "sk-***REDACTED***"},
		{"aws access key", "AKIAIOSFODNN7EXAMPLE", "AKIA***REDACTED***"},
		{"github pat", "ghp_" + strings.Repeat("a", 36), "ghp_***REDACTED***"},
		{"github server", "ghs_" + strings.Repeat("b", 36), "ghs_***REDACTED***"},
		{"jwt token", "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.abc123def456", "***REDACTED_JWT***"},
		{"bearer token", "Bearer eyJhbGciOiJIUzI1NiJ9abcdef1234567890", "Bearer ***REDACTED***"},
		{"api key pattern", `api_key="abcdefghij1234567890extra"`, "api_key: ***REDACTED***"},
		{"token pattern", `token = "abcdefghij1234567890extra"`, "token: ***REDACTED***"},
		{"password pattern", `password="mysecretpassword123"`, "password: ***REDACTED***"},
		{"secret pattern", `secret=verylongsecretvalue99`, "secret: ***REDACTED***"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SanitizeCredentials(tt.input)
			assert.Contains(t, result, tt.check)
			assert.NotContains(t, result, tt.input[len(tt.input)/2:])
		})
	}
}

func TestSanitizeCredentials_EmptyAndSafe(t *testing.T) {
	assert.Equal(t, "", SanitizeCredentials(""))
	assert.Equal(t, "hello world", SanitizeCredentials("hello world"))
}

func TestTruncateString(t *testing.T) {
	assert.Equal(t, "", TruncateString("", 100))
	assert.Equal(t, "short", TruncateString("short", 100))

	long := strings.Repeat("a", 200)
	result := TruncateString(long, 50)
	assert.True(t, strings.HasSuffix(result, "... [truncated]"))
	assert.LessOrEqual(t, len([]rune(result)), 50)

	tiny := TruncateString(long, 5)
	assert.Equal(t, "aaaaa", tiny)
}

func TestGetMaxPromptSize_Default(t *testing.T) {
	t.Setenv("REVENIUM_MAX_PROMPT_SIZE", "")
	assert.Equal(t, DefaultMaxPromptSize, GetMaxPromptSize())
}

func TestGetMaxPromptSize_EnvVar(t *testing.T) {
	t.Setenv("REVENIUM_MAX_PROMPT_SIZE", "1024")
	assert.Equal(t, 1024, GetMaxPromptSize())
}

func TestGetMaxPromptSize_InvalidEnvVar(t *testing.T) {
	t.Setenv("REVENIUM_MAX_PROMPT_SIZE", "invalid")
	assert.Equal(t, DefaultMaxPromptSize, GetMaxPromptSize())
}

func TestShouldCapturePrompts_MetadataOverridesEnv(t *testing.T) {
	t.Setenv("REVENIUM_CAPTURE_PROMPTS", "false")
	assert.True(t, ShouldCapturePrompts(map[string]interface{}{"capturePrompts": true}))

	t.Setenv("REVENIUM_CAPTURE_PROMPTS", "true")
	assert.False(t, ShouldCapturePrompts(map[string]interface{}{"capturePrompts": false}))
}

func TestShouldCapturePrompts_EnvVar(t *testing.T) {
	t.Setenv("REVENIUM_CAPTURE_PROMPTS", "true")
	assert.True(t, ShouldCapturePrompts(nil))

	t.Setenv("REVENIUM_CAPTURE_PROMPTS", "false")
	assert.False(t, ShouldCapturePrompts(nil))
}

func TestShouldCapturePrompts_DefaultFalse(t *testing.T) {
	t.Setenv("REVENIUM_CAPTURE_PROMPTS", "")
	assert.False(t, ShouldCapturePrompts(nil))
}

func TestExtractPrompts_Disabled(t *testing.T) {
	t.Setenv("REVENIUM_CAPTURE_PROMPTS", "")
	result := ExtractPrompts([]interface{}{}, "response", nil)
	assert.Nil(t, result)
}

func TestExtractPrompts_WithMessages(t *testing.T) {
	t.Setenv("REVENIUM_CAPTURE_PROMPTS", "true")
	messages := []interface{}{
		map[string]interface{}{"role": "system", "content": "You are helpful"},
		map[string]interface{}{"role": "user", "content": "Hello"},
	}

	result := ExtractPrompts(messages, "Hi there!", nil)
	require.NotNil(t, result)
	assert.Equal(t, "You are helpful", result.SystemPrompt)
	assert.Contains(t, result.InputPrompt, "[user]\nHello")
	assert.Equal(t, "Hi there!", result.OutputText)
}

func TestExtractPrompts_JSONString(t *testing.T) {
	t.Setenv("REVENIUM_CAPTURE_PROMPTS", "true")
	jsonStr := `[{"role":"system","content":"Be concise"},{"role":"user","content":"Hi"}]`

	result := ExtractPrompts(jsonStr, "Response", nil)
	require.NotNil(t, result)
	assert.Equal(t, "Be concise", result.SystemPrompt)
	assert.Contains(t, result.InputPrompt, "[user]\nHi")
}

func TestExtractPrompts_SanitizesCredentials(t *testing.T) {
	t.Setenv("REVENIUM_CAPTURE_PROMPTS", "true")
	messages := []interface{}{
		map[string]interface{}{"role": "user", "content": "My key is sk-abcdefghij1234567890extra"},
	}

	result := ExtractPrompts(messages, "", nil)
	require.NotNil(t, result)
	assert.Contains(t, result.InputPrompt, "sk-***REDACTED***")
	assert.NotContains(t, result.InputPrompt, "sk-abcdefghij1234567890extra")
}

func TestExtractPrompts_ContentBlocks(t *testing.T) {
	t.Setenv("REVENIUM_CAPTURE_PROMPTS", "true")
	messages := []interface{}{
		map[string]interface{}{
			"role": "user",
			"content": []interface{}{
				map[string]interface{}{"type": "text", "text": "Describe this"},
				map[string]interface{}{"type": "image_url", "image_url": "http://example.com/img.png"},
			},
		},
	}

	result := ExtractPrompts(messages, "", nil)
	require.NotNil(t, result)
	assert.Contains(t, result.InputPrompt, "Describe this")
	assert.Contains(t, result.InputPrompt, "[IMAGE]")
}

func TestExtractPrompts_TypedMapSlice(t *testing.T) {
	t.Setenv("REVENIUM_CAPTURE_PROMPTS", "true")
	messages := []map[string]interface{}{
		{"role": "system", "content": "You are helpful"},
		{"role": "user", "content": "Hello"},
	}

	result := ExtractPrompts(messages, "Response", nil)
	require.NotNil(t, result)
	assert.Equal(t, "You are helpful", result.SystemPrompt)
	assert.Contains(t, result.InputPrompt, "[user]\nHello")
}

func TestExtractPrompts_TypedContentBlocks(t *testing.T) {
	t.Setenv("REVENIUM_CAPTURE_PROMPTS", "true")
	content := []map[string]interface{}{
		{"type": "text", "text": "Describe this"},
		{"type": "image_url", "image_url": "http://example.com/img.png"},
	}
	messages := []interface{}{
		map[string]interface{}{"role": "user", "content": content},
	}

	result := ExtractPrompts(messages, "", nil)
	require.NotNil(t, result)
	assert.Contains(t, result.InputPrompt, "Describe this")
	assert.Contains(t, result.InputPrompt, "[IMAGE]")
}

func TestSanitizeCredentials_BearerShortNotRedacted(t *testing.T) {
	result := SanitizeCredentials("Bearer of good news")
	assert.Equal(t, "Bearer of good news", result)
}

func TestSanitizeCredentials_PasswordNaturalLanguage(t *testing.T) {
	result := SanitizeCredentials("password reset successful")
	assert.Equal(t, "password reset successful", result)
}

func TestExtractPrompts_EmptyReturnsNil(t *testing.T) {
	t.Setenv("REVENIUM_CAPTURE_PROMPTS", "true")
	result := ExtractPrompts([]interface{}{}, "", nil)
	assert.Nil(t, result)
}
