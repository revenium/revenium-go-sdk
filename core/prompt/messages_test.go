package prompt

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func message(role, content string) map[string]interface{} {
	return map[string]interface{}{"role": role, "content": content}
}

func decodedMessages(t *testing.T, text string) []map[string]interface{} {
	t.Helper()
	var messages []map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(text), &messages))
	return messages
}

func TestSerializeMessagesWithinLimit_KeepsMessagesThatFitAsGiven(t *testing.T) {
	text, cut, err := SerializeMessagesWithinLimit([]interface{}{message("user", "a <b> & c")}, 1000)

	require.NoError(t, err)
	assert.False(t, cut)
	assert.Equal(t, `[{"content":"a <b> & c","role":"user"}]`, text)
}

func TestSerializeMessagesWithinLimit_ShortensTheLongestStringsFirst(t *testing.T) {
	messages := []interface{}{message("user", "short question"), message("assistant", strings.Repeat("x", 5000))}

	text, cut, err := SerializeMessagesWithinLimit(messages, 500)

	require.NoError(t, err)
	assert.True(t, cut)
	assert.LessOrEqual(t, utf8.RuneCountInString(text), 500)
	decoded := decodedMessages(t, text)
	require.Len(t, decoded, 2)
	assert.Equal(t, "short question", decoded[0]["content"])
	assert.True(t, strings.HasSuffix(decoded[1]["content"].(string), "...[TRUNCATED]"))
}

func TestSerializeMessagesWithinLimit_LeavesOutTheOldestMessagesWhenCuttingIsNotEnough(t *testing.T) {
	var messages []interface{}
	for i := 0; i < 40; i++ {
		messages = append(messages, message("user", strings.Repeat("m", 30)))
	}
	messages = append(messages, message("user", "newest"))

	text, cut, err := SerializeMessagesWithinLimit(messages, 300)

	require.NoError(t, err)
	assert.True(t, cut)
	assert.LessOrEqual(t, utf8.RuneCountInString(text), 300)
	decoded := decodedMessages(t, text)
	require.NotEmpty(t, decoded)
	assert.Less(t, len(decoded), len(messages))
	assert.Equal(t, "newest", decoded[len(decoded)-1]["content"])
}

func TestSerializeMessagesWithinLimit_SendsAnEmptyListWhenNoMessageFits(t *testing.T) {
	text, cut, err := SerializeMessagesWithinLimit([]interface{}{message("user", "hello there")}, 10)

	require.NoError(t, err)
	assert.True(t, cut)
	assert.Equal(t, "[]", text)
}

func TestSanitizeCredentialsIn_RedactsSecretsInValuesKeysAndNestedJSON(t *testing.T) {
	messages := []interface{}{
		map[string]interface{}{"role": "user", "content": "my key is sk-ant-abcdefghij1234567890extra"},
		map[string]interface{}{"role": "assistant", "tool_calls": []interface{}{
			map[string]interface{}{"function": map[string]interface{}{"arguments": `{"password":"hunter2hunter2","city":"Paris"}`}},
		}},
		map[string]interface{}{"role": "user", "api_key": "plainvalue1234567890abc"},
	}

	masked, err := json.Marshal(SanitizeCredentialsIn(messages))
	require.NoError(t, err)

	text := string(masked)
	assert.NotContains(t, text, "abcdefghij1234567890extra")
	assert.NotContains(t, text, "hunter2hunter2")
	assert.NotContains(t, text, "plainvalue1234567890abc")
	assert.Contains(t, text, "Paris")
}

func TestSanitizeCredentialsIn_LeavesTheCallersValuesUnchanged(t *testing.T) {
	original := map[string]interface{}{"content": "sk-ant-abcdefghij1234567890extra"}

	SanitizeCredentialsIn([]interface{}{original})

	assert.Equal(t, "sk-ant-abcdefghij1234567890extra", original["content"])
}

func TestSanitizeCredentials_KeepsJSONTextValid(t *testing.T) {
	masked := SanitizeCredentials(`{"token": "abcdefghijklmnopqrstuvwxyz", "note": "fine"}`)

	var decoded map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(masked), &decoded))
	assert.Equal(t, "fine", decoded["note"])
	assert.NotContains(t, masked, "abcdefghijklmnopqrstuvwxyz")
}

func TestContentText(t *testing.T) {
	assert.Equal(t, "plain", ContentText("plain"))
	assert.Equal(t, `[{"text":"a","type":"text"}]`, ContentText([]interface{}{map[string]interface{}{"type": "text", "text": "a"}}))
	assert.Equal(t, "", ContentText(nil))
}

func TestReplyCollector_KeepsARedactionMarginPastTheLimitAndReportsTheCut(t *testing.T) {
	t.Setenv("REVENIUM_MAX_PROMPT_SIZE", "10")
	collector := NewReplyCollector()

	collector.Add(strings.Repeat("é", redactionMargin))
	assert.False(t, collector.Truncated())
	collector.Add("wörld and more")

	assert.Equal(t, 10+redactionMargin, utf8.RuneCountInString(collector.Text()))
	assert.True(t, collector.Truncated())
}

func TestMarkTruncated_EndsInTheMarkerWithinTheLimit(t *testing.T) {
	assert.Equal(t, "abcdef...[TRUNCATED]", MarkTruncated("abcdefghij", 20))
	assert.Equal(t, "short...[TRUNCATED]", MarkTruncated("short", 20))
}

func TestReplyCollector_NilCollectorIgnoresText(t *testing.T) {
	var collector *ReplyCollector

	collector.Add("ignored")

	assert.Equal(t, "", collector.Text())
}

func TestWithoutInlineData_ReplacesDataURIsAndBase64Payloads(t *testing.T) {
	messages := []interface{}{
		map[string]interface{}{"role": "user", "content": []interface{}{
			map[string]interface{}{"type": "image_url", "image_url": map[string]interface{}{"url": "data:image/png;base64,iVBORw0KGgo="}},
			map[string]interface{}{"type": "image", "source": map[string]interface{}{"type": "base64", "media_type": "image/png", "data": "iVBORw0KGgo="}},
			map[string]interface{}{"type": "input_audio", "input_audio": map[string]interface{}{"data": "UklGRg==", "format": "wav"}},
			map[string]interface{}{"type": "image_url", "image_url": map[string]interface{}{"url": "https://example.com/cat.png"}},
			map[string]interface{}{"type": "text", "text": "data: not a file"},
		}},
	}

	encoded, err := json.Marshal(WithoutInlineData(messages))
	require.NoError(t, err)

	text := string(encoded)
	assert.NotContains(t, text, "iVBORw0KGgo")
	assert.NotContains(t, text, "UklGRg")
	assert.Equal(t, 3, strings.Count(text, BinaryPlaceholder))
	assert.Contains(t, text, "https://example.com/cat.png")
	assert.Contains(t, text, "data: not a file")
}

func TestWithoutInlineData_KeepsOrdinaryObjectsThatNameDataAndFormat(t *testing.T) {
	toolInput := map[string]interface{}{"type": "tool_use", "input": map[string]interface{}{"format": "csv", "data": "name\nAlice"}}
	audio := map[string]interface{}{"type": "input_audio", "input_audio": map[string]interface{}{"data": "UklGRg==", "format": "wav"}}

	kept := WithoutInlineData([]interface{}{toolInput, audio}).([]interface{})

	assert.Equal(t, toolInput, kept[0])
	assert.Equal(t, BinaryPlaceholder, kept[1].(map[string]interface{})["input_audio"].(map[string]interface{})["data"])
}

func TestSerializeMessagesWithinLimit_MarksEveryStringItCutEvenAtTheFloor(t *testing.T) {
	var messages []interface{}
	for i := 0; i < 30; i++ {
		messages = append(messages, message("user", strings.Repeat("m", 100)))
	}

	text, cut, err := SerializeMessagesWithinLimit(messages, 200)

	require.NoError(t, err)
	require.True(t, cut)
	decoded := decodedMessages(t, text)
	require.NotEmpty(t, decoded)
	for _, kept := range decoded {
		assert.True(t, strings.HasSuffix(kept["content"].(string), "...[TRUNCATED]"), kept["content"])
	}
}

func TestWithoutInlineData_ReplacesInlineFiles(t *testing.T) {
	messages := []interface{}{
		map[string]interface{}{"type": "file", "file": map[string]interface{}{"file_data": "JVBERi0xLjQK", "filename": "a.pdf"}},
		map[string]interface{}{"type": "input_file", "file_data": "JVBERi0xLjQK", "filename": "a.pdf"},
		map[string]interface{}{"type": "tool_use", "input": map[string]interface{}{"file_data": "keep me"}},
	}

	kept := WithoutInlineData(messages).([]interface{})

	assert.Equal(t, BinaryPlaceholder, kept[0].(map[string]interface{})["file"].(map[string]interface{})["file_data"])
	assert.Equal(t, BinaryPlaceholder, kept[1].(map[string]interface{})["file_data"])
	assert.Equal(t, messages[2], kept[2])
}
