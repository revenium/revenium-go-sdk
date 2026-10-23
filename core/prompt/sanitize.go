package prompt

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	redacted          = "***REDACTED***"
	redactedJWT       = "***REDACTED_JWT***"
	maxLabelLength    = 128
	minSecretLength   = 20
	minPasswordLength = 8
)

// pythonSpace is Python's \s for a character class. Go's \s is ASCII only and leaves out
// \v, so a credential after a non-breaking space would pass here and be masked by Python.
const pythonSpace = `\s\v\x{85}\x{1c}-\x{1f}\p{Z}`

var (
	bearerCredential   = regexp.MustCompile(`(?i)(Bearer[` + pythonSpace + `]+)[a-zA-Z0-9_\-.+/=]{8,}`)
	basicCredential    = regexp.MustCompile(`(?i)(Basic[` + pythonSpace + `]+)([A-Za-z0-9+/]{8,}={0,2})`)
	cutBasicCredential = regexp.MustCompile(`(?i)(Basic[` + pythonSpace + `]+)[A-Za-z0-9+/]{8,}={0,2}$`)
	signedURLParameter = regexp.MustCompile(`(?i)([?&](?:X-Amz-Signature|X-Amz-Security-Token|X-Goog-Signature|Signature|sig)=)[^&#'"` + pythonSpace + `]+`)
	urlUserPassword    = regexp.MustCompile(`(://[^/:@` + pythonSpace + `]{0,256}:)[^/@` + pythonSpace + `]{1,256}(@)`)
)

// SanitizeCredentials redacts API keys, tokens, passwords, private keys, card numbers
// and other credentials in captured text, as the Python SDK's sanitize_credentials does.
// Text holding a JSON object or array is masked value by value and re-encoded, so it
// stays valid JSON; text with nothing to redact is returned unchanged.
func SanitizeCredentials(text string) string {
	return sanitizeJSONOrText(text, SanitizeCredentials, SanitizeText)
}

// SanitizeCutText is SanitizeCredentials for text that was cut short, such as a streamed
// reply kept only up to its capture budget. A Basic credential running into the end of
// the text, or into the end of any string in it when it holds JSON, is masked too,
// although its cut base64 no longer decodes to the user:password that SanitizeCredentials
// requires. A cut tool input sits inside such a string.
func SanitizeCutText(text string) string {
	return sanitizeJSONOrText(text, SanitizeCutText, sanitizeCutPlainText)
}

func sanitizeCutPlainText(text string) string {
	return maskCutBasicCredential(SanitizeText(text))
}

func maskCutBasicCredential(text string) string {
	loc := cutBasicCredential.FindStringSubmatchIndex(text)
	if loc == nil || !basicStarts(text, loc[0]) {
		return text
	}
	return text[:loc[3]] + redacted
}

func sanitizeJSONOrText(text string, sanitizeString, sanitizePlain func(string) string) string {
	if text == "" {
		return text
	}
	if parsed, ok := jsonContainer(text); ok {
		masked := sanitizeNode(parsed, nil, sanitizeString)
		if reflect.DeepEqual(masked, parsed) {
			return text
		}
		if encoded, err := encodeJSON(masked); err == nil {
			return encoded
		}
	}
	return sanitizePlain(text)
}

// SanitizeCredentialsIn returns a copy of a decoded JSON value with credentials redacted
// in every key and string, and the value of every secret-named key (a password, token,
// api key or similar) replaced.
func SanitizeCredentialsIn(node interface{}) interface{} {
	return sanitizeNode(node, nil, SanitizeCredentials)
}

func sanitizeNode(node interface{}, key *string, sanitizeString func(string) string) interface{} {
	switch value := node.(type) {
	case map[string]interface{}:
		masked := make(map[string]interface{}, len(value))
		for childKey, child := range value {
			childKey := childKey
			masked[SanitizeText(childKey)] = sanitizeNode(child, &childKey, sanitizeString)
		}
		return masked
	case []interface{}:
		masked := make([]interface{}, len(value))
		for i, child := range value {
			masked[i] = sanitizeNode(child, key, sanitizeString)
		}
		return masked
	case nil, bool:
		return node
	}
	if key != nil && isSecretValue(*key, fmt.Sprint(node)) {
		return redacted
	}
	if text, ok := node.(string); ok {
		return sanitizeString(text)
	}
	if text := fmt.Sprint(node); SanitizeText(text) != text {
		return redacted
	}
	return node
}

func isSecretValue(key, value string) bool {
	if isPasswordLabel(key) {
		return utf8.RuneCountInString(value) >= minPasswordLength
	}
	return isSecretLabel(key) && secretValueLength(value) >= minSecretLength
}

func jsonContainer(text string) (interface{}, bool) {
	trimmed := strings.TrimLeftFunc(text, unicode.IsSpace)
	if !strings.HasPrefix(trimmed, "{") && !strings.HasPrefix(trimmed, "[") {
		return nil, false
	}
	value, err := decodeJSON([]byte(text))
	return value, err == nil
}

// SanitizeText redacts credentials in plain text with the Python SDK's shapes and label
// rules and no JSON handling, so any text sent to Revenium, captured prompts or error
// text, goes through the same matcher.
func SanitizeText(text string) string {
	text = maskSecretShapes(text)
	text = maskJWTs(text)
	text = bearerCredential.ReplaceAllString(text, "${1}"+redacted)
	text = maskBasicCredentials(text)
	text = signedURLParameter.ReplaceAllString(text, "${1}"+redacted)
	text = urlUserPassword.ReplaceAllString(text, "${1}"+redacted+"${2}")
	return maskLabelledValues(text)
}

// maskBasicCredentials masks an HTTP Basic credential only when it decodes to user:password.
func maskBasicCredentials(text string) string {
	var out strings.Builder
	last := 0
	for from := 0; from < len(text); {
		loc := basicCredential.FindStringSubmatchIndex(text[from:])
		if loc == nil {
			break
		}
		start, end := from+loc[0], from+loc[1]
		if !basicStarts(text, start) || (end < len(text) && strings.IndexByte("+/=", text[end]) >= 0) || (end < len(text) && isASCIIAlnum(text[end])) {
			from = start + 1
			continue
		}
		credential := text[from+loc[4] : from+loc[5]]
		if decoded, err := base64.StdEncoding.DecodeString(credential); err == nil && bytes.IndexByte(decoded, ':') >= 0 {
			out.WriteString(text[last:from+loc[3]] + redacted)
			last = end
		}
		from = end
	}
	if last == 0 {
		return text
	}
	out.WriteString(text[last:])
	return out.String()
}

func basicStarts(text string, i int) bool {
	if i == 0 || (!isASCIIAlnum(text[i-1]) && text[i-1] != '_') {
		return true
	}
	return followsEscape(text, i)
}

// maskLabelledValues masks the value after every label ending in a secret word, such as
// AWS_SECRET_ACCESS_KEY=... or "password": "...", keeping the label and its separator.
func maskLabelledValues(text string) string {
	var out strings.Builder
	position := 0
	for {
		labelStart, labelEnd, ok := nextLabel(text, position)
		if !ok {
			break
		}
		label := text[labelStart:labelEnd.word]
		valueStart, valueEnd, isValue := labelledValue(text, labelEnd, label)
		if isValue {
			out.WriteString(text[position:valueStart] + redacted)
			position = valueEnd
		} else {
			out.WriteString(text[position:labelEnd.separator])
			position = labelEnd.separator
		}
	}
	if position == 0 {
		return text
	}
	out.WriteString(text[position:])
	return out.String()
}

// labelEnds marks where a label's word stops and where its value starts, both after the
// whole separator run and after the run's first symbol alone.
type labelEnds struct {
	word           int
	separator      int
	firstSeparator int
}

// labelledValue redacts the wider of the two readings of a separator run: "==hunter2"
// reads as the eight-character "=hunter2" after one '=' and as the too-short "hunter2"
// after both, and only the first reading clears the password length floor.
func labelledValue(text string, ends labelEnds, label string) (int, int, bool) {
	runEnd, afterRun := labelledValueEnd(text, ends.separator, label)
	firstEnd, afterFirst := labelledValueEnd(text, ends.firstSeparator, label)
	switch {
	case afterFirst && afterRun:
		return ends.firstSeparator, max(firstEnd, runEnd), true
	case afterFirst:
		return ends.firstSeparator, firstEnd, true
	default:
		return ends.separator, runEnd, afterRun
	}
}

func nextLabel(text string, from int) (int, labelEnds, bool) {
	for i := from; i < len(text); i++ {
		if !labelStarts(text, i) {
			continue
		}
		if ends, ok := labelAt(text, i); ok {
			return i, ends, true
		}
	}
	return 0, labelEnds{}, false
}

// labelStarts is Python's start of a label: not inside a run of key characters, and not on
// the letter of a serialized "\n", "\r" or "\t" escape; or right after such an escape.
func labelStarts(text string, i int) bool {
	if followsEscape(text, i) {
		return true
	}
	if i > 0 && isKeyChar(text[i-1]) {
		return false
	}
	return !(i > 0 && text[i-1] == '\\' && strings.IndexByte("nrt", text[i]) >= 0)
}

func labelAt(text string, i int) (labelEnds, bool) {
	word := i
	for word < len(text) && isKeyChar(text[word]) {
		word++
	}
	if word == i || word-i > maxLabelLength {
		return labelEnds{}, false
	}
	at := skipQuote(text, word)
	at = skipSpace(text, at)
	if at >= len(text) || (text[at] != ':' && text[at] != '=') {
		return spacedSecretLabel(text, i, word, at)
	}
	return labelEnds{
		word:           word,
		separator:      skipQuote(text, skipSpace(text, skipSeparatorRun(text, at))),
		firstSeparator: skipQuote(text, skipSpace(text, at+1)),
	}, true
}

// spacedSecretLabel accepts a token or key label followed by only a space or quote, such as
// "Authorization: token 0123..." or "apikey abc...". The Python SDK needs ':' or '=' here;
// Go's earlier redaction caught these and still does. Password labels keep needing a
// separator, so prose like "password reset" is not read as a label.
func spacedSecretLabel(text string, start, word, at int) (labelEnds, bool) {
	label := text[start:word]
	if at == word || isPasswordLabel(label) || !isSecretLabel(label) {
		return labelEnds{}, false
	}
	valueStart := skipQuote(text, at)
	return labelEnds{word: word, separator: valueStart, firstSeparator: valueStart}, true
}

// skipSeparatorRun steps over a run of ':', '=' and '>', so a value assigned with Go's
// ":=", a "=>" arrow or "==" is read from its first character, not from the second symbol.
func skipSeparatorRun(text string, i int) int {
	for i < len(text) && strings.IndexByte(":=>", text[i]) >= 0 {
		i++
	}
	return i
}

func skipQuote(text string, i int) int {
	if i < len(text) && (text[i] == '"' || text[i] == '\'') {
		return i + 1
	}
	if i+1 < len(text) && text[i] == '\\' && (text[i+1] == '"' || text[i+1] == '\'') {
		return i + 2
	}
	return i
}

func skipSpace(text string, i int) int {
	for i < len(text) {
		r, size := utf8.DecodeRuneInString(text[i:])
		if !isUnicodeSpace(r) {
			break
		}
		i += size
	}
	return i
}

func labelledValueEnd(text string, start int, label string) (int, bool) {
	if isPasswordLabel(label) {
		return passwordValueEnd(text, start)
	}
	if isSecretLabel(label) {
		length := secretValueLength(text[start:])
		return start + length, length >= minSecretLength
	}
	return 0, false
}

// passwordValueEnd matches eight or more characters that are not quotes, spaces or a lone
// backslash (an escape counts as one), ending neither on a JSON delimiter nor on an
// escaped quote, so the JSON around a masked password stays valid.
func passwordValueEnd(text string, start int) (int, bool) {
	var ends []int
	for i := start; i < len(text); {
		r, size := utf8.DecodeRuneInString(text[i:])
		switch {
		case r == '\\':
			next, nextSize := utf8.DecodeRuneInString(text[i+size:])
			if i+size >= len(text) || next == '\n' {
				return passwordEnd(text, start, ends)
			}
			i += size + nextSize
		case r == '"' || r == '\'' || isUnicodeSpace(r):
			return passwordEnd(text, start, ends)
		default:
			i += size
		}
		ends = append(ends, i)
	}
	return passwordEnd(text, start, ends)
}

func passwordEnd(text string, start int, ends []int) (int, bool) {
	for k := len(ends); k >= minPasswordLength; k-- {
		end := ends[k-1]
		value := text[start:end]
		if strings.ContainsRune(",{}[]", rune(value[len(value)-1])) || strings.HasSuffix(value, `\"`) || strings.HasSuffix(value, `\'`) {
			continue
		}
		return end, true
	}
	return 0, false
}

func secretValueLength(text string) int {
	length := 0
	for length < len(text) && (isASCIIAlnum(text[length]) || strings.IndexByte("_-.+/=", text[length]) >= 0) {
		length++
	}
	return length
}

func isKeyChar(c byte) bool {
	return isASCIIAlnum(c) || c == '_' || c == '-'
}

// isUnicodeSpace matches Python's \s, which also counts the separators \x1c to \x1f.
func isUnicodeSpace(r rune) bool {
	return unicode.IsSpace(r) || (r >= 0x1c && r <= 0x1f)
}
