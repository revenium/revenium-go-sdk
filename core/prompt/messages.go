package prompt

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"unicode/utf8"
)

var errTrailingJSON = errors.New("text continues after the JSON value")

const (
	emptyMessageList = "[]"
	maxSearchSteps   = 40
)

// SerializeMessagesWithinLimit encodes messages as a JSON array of at most
// maxLength characters and reports whether anything was cut. Messages that fit
// are encoded as given. Otherwise the longest strings are shortened first, each
// ending in the truncation marker, so short turns stay whole; if that is not
// enough the oldest messages are left out. The result is always valid JSON.
func SerializeMessagesWithinLimit(messages []interface{}, maxLength int) (string, bool, error) {
	whole, err := encodeJSON(messages)
	if err != nil {
		return "", false, err
	}
	if utf8.RuneCountInString(whole) <= maxLength {
		return whole, false, nil
	}
	text, fits := cutToFit(messages, maxLength)
	if !fits {
		text, fits = cutToFit(newestMessagesThatFit(messages, maxLength), maxLength)
	}
	if !fits {
		text = emptyMessageList
	}
	return text, true, nil
}

// cutFloor is the shortest a cut string gets: one character and the marker, so every
// shortened string still shows it was cut.
func cutFloor() int {
	return utf8.RuneCountInString(truncationSuffix) + 1
}

func cutToFit(messages []interface{}, maxLength int) (string, bool) {
	floor := cutFloor()
	if encodedLength(cutStrings(messages, floor)) > maxLength {
		return "", false
	}
	fits := floor
	tooLong := max(floor, min(longestString(messages)+1, maxLength+1))
	for step := 0; step < maxSearchSteps && tooLong-fits > 1; step++ {
		middle := (fits + tooLong) / 2
		if encodedLength(cutStrings(messages, middle)) <= maxLength {
			fits = middle
		} else {
			tooLong = middle
		}
	}
	text, _ := encodeJSON(cutStrings(messages, fits))
	return text, true
}

func newestMessagesThatFit(messages []interface{}, maxLength int) []interface{} {
	floor := cutFloor()
	total := len(emptyMessageList)
	kept := 0
	for i := len(messages) - 1; i >= 0; i-- {
		total += encodedLength(cutStrings(messages[i], floor))
		if kept > 0 {
			total++
		}
		if total > maxLength {
			break
		}
		kept++
	}
	return messages[len(messages)-kept:]
}

func cutStrings(node interface{}, keep int) interface{} {
	switch value := node.(type) {
	case string:
		return TruncateString(value, keep)
	case map[string]interface{}:
		cut := make(map[string]interface{}, len(value))
		for key, child := range value {
			cut[key] = cutStrings(child, keep)
		}
		return cut
	case []interface{}:
		cut := make([]interface{}, len(value))
		for i, child := range value {
			cut[i] = cutStrings(child, keep)
		}
		return cut
	default:
		return node
	}
}

func longestString(node interface{}) int {
	longest := 0
	switch value := node.(type) {
	case string:
		return utf8.RuneCountInString(value)
	case map[string]interface{}:
		for _, child := range value {
			longest = max(longest, longestString(child))
		}
	case []interface{}:
		for _, child := range value {
			longest = max(longest, longestString(child))
		}
	}
	return longest
}

func encodedLength(node interface{}) int {
	text, err := encodeJSON(node)
	if err != nil {
		return 0
	}
	return utf8.RuneCountInString(text)
}

// ContentText is a message's content as text: a string as is, and a list of
// content parts as JSON.
func ContentText(content interface{}) string {
	switch value := content.(type) {
	case string:
		return value
	case []interface{}:
		encoded, err := encodeJSON(value)
		if err != nil {
			return ""
		}
		return encoded
	default:
		return ""
	}
}

// BinaryPlaceholder stands in for inline file data in captured messages.
const BinaryPlaceholder = "[BINARY_DATA]"

// WithoutInlineData returns a copy of decoded messages with inline file data, data URIs
// and base64 image, document or audio payloads, replaced by BinaryPlaceholder. A data
// URI is the whole file, megabytes per image, which capture would otherwise encode, scan
// and carry to Revenium on the caller's request.
func WithoutInlineData(node interface{}) interface{} {
	switch value := node.(type) {
	case map[string]interface{}:
		kept := make(map[string]interface{}, len(value))
		for key, child := range value {
			kept[key] = WithoutInlineData(child)
		}
		if isBase64Source(value) {
			kept["data"] = BinaryPlaceholder
		}
		if audio, ok := kept["input_audio"].(map[string]interface{}); ok && value["type"] == "input_audio" {
			replaceIfPresent(audio, "data")
		}
		if value["type"] == "input_file" {
			replaceIfPresent(kept, "file_data")
		}
		if file, ok := kept["file"].(map[string]interface{}); ok && value["type"] == "file" {
			replaceIfPresent(file, "file_data")
		}
		return kept
	case []interface{}:
		kept := make([]interface{}, len(value))
		for i, child := range value {
			kept[i] = WithoutInlineData(child)
		}
		return kept
	case string:
		if isDataURI(value) {
			return BinaryPlaceholder
		}
	}
	return node
}

func replaceIfPresent(fields map[string]interface{}, key string) {
	if _, present := fields[key]; present {
		fields[key] = BinaryPlaceholder
	}
}

// isBase64Source is an Anthropic image or document source carrying the file itself.
func isBase64Source(fields map[string]interface{}) bool {
	_, hasMediaType := fields["media_type"]
	_, hasData := fields["data"]
	return fields["type"] == "base64" && hasMediaType && hasData
}

func isDataURI(text string) bool {
	if !strings.HasPrefix(text, "data:") {
		return false
	}
	header, _, found := strings.Cut(text, ",")
	return found && strings.HasSuffix(header, ";base64")
}

// DecodeJSONValues converts any JSON-encodable value, such as a provider SDK's
// typed message list, into the plain values it is sent as.
func DecodeJSONValues(value interface{}) (interface{}, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return decodeJSON(encoded)
}

// decodeJSON decodes exactly one JSON value, keeping numbers as json.Number so a card
// number survives for redaction and an untouched number is re-encoded as sent.
func decodeJSON(data []byte) (interface{}, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var value interface{}
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return nil, errTrailingJSON
	}
	return value, nil
}

func encodeJSON(value interface{}) (string, error) {
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return "", err
	}
	return strings.TrimSuffix(buf.String(), "\n"), nil
}
