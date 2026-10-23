package prompt

import (
	"regexp"
	"strings"
)

// The shapes, labels and their order follow revenium_middleware/_core/secret_shapes.py in
// the Python SDK, so both SDKs redact the same credentials. Go's regexp has no lookaround,
// so the Python lookbehind and lookahead conditions are checked in code around each match.

// pemBodyMaxTokens is the {0,16384} bound on body tokens in the Python SDK's
// _PEM_PRIVATE_KEY pattern (revenium_middleware/_core/secret_shapes.py): how far past a
// BEGIN ... PRIVATE KEY header the masked span reaches. Lowering it leaves the tail of a
// long key unmasked.
const pemBodyMaxTokens = 16384

type shapeMatch struct {
	start, keepEnd, end int
}

type secretShape struct {
	find     func(text string, from int) (shapeMatch, bool)
	isSecret func(match string) bool
}

var secretShapes = []secretShape{
	{find: findPEMPrivateKey},
	prefixedShape(`pplx-`, `[a-zA-Z0-9_-]{20,}`),
	prefixedShape(`sk-proj-`, `[a-zA-Z0-9_-]{48,}`),
	prefixedShape(`sk-ant-`, `[a-zA-Z0-9_-]{20,}`),
	prefixedShape(`sk-`, `[a-zA-Z0-9_-]{20,}`),
	prefixedShape(`AKIA`, `[A-Z0-9]{16}`),
	prefixedShape(`ghp_`, `[a-zA-Z0-9]{36,}`),
	prefixedShape(`ghs_`, `[a-zA-Z0-9]{36,}`),
	prefixedShape(`gh[our]_`, `[a-zA-Z0-9]{36,}`),
	prefixedShape(`github_pat_`, `[A-Za-z0-9_]{22,}`),
	prefixedShape(`AIza`, `[A-Za-z0-9_-]{30,}`),
	prefixedShape(`xai-`, `[A-Za-z0-9]{20,}`),
	prefixedShape(`gsk_`, `[A-Za-z0-9]{20,}`),
	prefixedShape(`xox[abprse]-`, `[0-9]{6,}-[A-Za-z0-9-]{8,}`),
	prefixedShape(`hf_`, `[A-Za-z0-9]{30,}`),
	prefixedShape(`rev_sk_`, `[A-Za-z0-9_-]{16,}`),
	prefixedShape(`rev_mk_`, `[A-Za-z0-9_-]{16,}`),
	prefixedShape(`hak_`, `[A-Za-z0-9_-]{16,}`),
	prefixedShape(`[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}:`, `[0-9a-f]{32,}`),
	{find: findLongGroupedCardNumber, isSecret: passesLuhn},
	{find: findCardNumber, isSecret: passesLuhn},
}

var (
	pemHeader = regexp.MustCompile(`-----BEGIN [A-Z ]{0,40}PRIVATE KEY-----`)
	pemFooter = regexp.MustCompile(`^-----END [A-Z ]{0,40}PRIVATE KEY-----`)
	dottedRun = regexp.MustCompile(`[a-zA-Z0-9_-]+(?:\.[a-zA-Z0-9_-]+){2,}`)
)

// prefixedShape finds the prefix alone, checks the run boundary before it, and only then
// matches the body anchored after it. A rejected candidate costs no more than its prefix,
// so the scan stays linear even on text built of near-miss prefixes such as "ask-ask-...".
func prefixedShape(prefix, body string) secretShape {
	prefixPattern := regexp.MustCompile(prefix)
	bodyPattern := regexp.MustCompile(`^(?:` + body + `)`)
	return secretShape{find: func(text string, from int) (shapeMatch, bool) {
		for from < len(text) {
			loc := prefixPattern.FindStringIndex(text[from:])
			if loc == nil {
				return shapeMatch{}, false
			}
			start, keepEnd := from+loc[0], from+loc[1]
			if startsRun(text, start) {
				if bodyEnd := bodyPattern.FindStringIndex(text[keepEnd:]); bodyEnd != nil {
					return shapeMatch{start: start, keepEnd: keepEnd, end: keepEnd + bodyEnd[1]}, true
				}
			}
			from = start + 1
		}
		return shapeMatch{}, false
	}}
}

// startsRun is Python's (?<![A-Za-z0-9](?<!\\[nrt])): no letter or digit before i, except
// the letter of a serialized "\n", "\r" or "\t" escape.
func startsRun(text string, i int) bool {
	if i == 0 || !isASCIIAlnum(text[i-1]) {
		return true
	}
	return followsEscape(text, i)
}

func followsEscape(text string, i int) bool {
	return i >= 2 && text[i-2] == '\\' && strings.IndexByte("nrt", text[i-1]) >= 0
}

func findPEMPrivateKey(text string, from int) (shapeMatch, bool) {
	loc := pemHeader.FindStringIndex(text[from:])
	if loc == nil {
		return shapeMatch{}, false
	}
	start, keepEnd := from+loc[0], from+loc[1]
	end := pemBodyEnd(text, keepEnd)
	if footer := pemFooter.FindStringIndex(text[end:]); footer != nil {
		end += footer[1]
	}
	return shapeMatch{start: start, keepEnd: keepEnd, end: end}, true
}

func pemBodyEnd(text string, i int) int {
	for tokens := 0; tokens < pemBodyMaxTokens && i < len(text); tokens++ {
		c := text[i]
		switch {
		case isASCIIAlnum(c) || strings.IndexByte("+/=,", c) >= 0:
			i++
		case c == ':':
			i = skipBlanks(text, i+1)
		case c == '-' && (i+1 == len(text) || text[i+1] != '-'):
			i++
		case c == '\r' || c == '\n':
			i = skipBlanks(text, i+1)
		case c == '\\' && i+1 < len(text) && strings.IndexByte("nrt", text[i+1]) >= 0:
			i = skipBlanks(text, i+2)
		default:
			return i
		}
	}
	return i
}

func skipBlanks(text string, i int) int {
	for i < len(text) && (text[i] == ' ' || text[i] == '\t') {
		i++
	}
	return i
}

func findCardNumber(text string, from int) (shapeMatch, bool) {
	return findCard(text, from, func(text string, i int) (int, bool) {
		if end, ok := ungroupedCard(text, i); ok {
			return end, true
		}
		if end, ok := fourCardGroups(text, i); ok && cardEnds(text, end) {
			return end, true
		}
		return amexGroups(text, i)
	})
}

// findLongGroupedCardNumber is a 17 to 19 digit card in four-digit groups, tried before
// the 16 digit form so a card followed by an expiry or a CVV is still masked.
func findLongGroupedCardNumber(text string, from int) (shapeMatch, bool) {
	return findCard(text, from, func(text string, i int) (int, bool) {
		end, ok := fourCardGroups(text, i)
		if !ok || end >= len(text) || text[end] != text[i+4] {
			return 0, false
		}
		for extra := 3; extra >= 1; extra-- {
			if digitsAt(text, end+1, extra) && cardEnds(text, end+1+extra) {
				return end + 1 + extra, true
			}
		}
		return 0, false
	})
}

func findCard(text string, from int, body func(text string, i int) (int, bool)) (shapeMatch, bool) {
	for i := from; i < len(text); i++ {
		if text[i] < '2' || text[i] > '6' || !cardStarts(text, i) {
			continue
		}
		if end, ok := body(text, i); ok {
			return shapeMatch{start: i, keepEnd: i, end: end}, true
		}
	}
	return shapeMatch{}, false
}

func cardStarts(text string, i int) bool {
	if !startsRun(text, i) {
		return false
	}
	if i >= 2 && text[i-1] == '.' && isDigit(text[i-2]) {
		return false
	}
	return !(i >= 5 && isCardSeparator(text[i-1]) && digitsAt(text, i-5, 4))
}

func cardEnds(text string, end int) bool {
	if end < len(text) && isASCIIAlnum(text[end]) {
		return false
	}
	if end+1 < len(text) && text[end] == '.' && isDigit(text[end+1]) {
		return false
	}
	return !(end < len(text) && isCardSeparator(text[end]) && digitsAt(text, end+1, 4))
}

func ungroupedCard(text string, i int) (int, bool) {
	end := i
	for end < len(text) && isDigit(text[end]) {
		end++
	}
	digits := end - i
	return end, digits >= 13 && digits <= 19 && cardEnds(text, end)
}

func fourCardGroups(text string, i int) (int, bool) {
	if !digitsAt(text, i, 4) || i+4 >= len(text) || !isCardSeparator(text[i+4]) {
		return 0, false
	}
	separator := text[i+4]
	for group := 1; group <= 3; group++ {
		at := i + 5*group
		if !digitsAt(text, at, 4) || (group < 3 && (at+4 >= len(text) || text[at+4] != separator)) {
			return 0, false
		}
	}
	return i + 19, true
}

func amexGroups(text string, i int) (int, bool) {
	if !digitsAt(text, i, 4) || i+4 >= len(text) || !isCardSeparator(text[i+4]) {
		return 0, false
	}
	separator := text[i+4]
	if !digitsAt(text, i+5, 6) || i+11 >= len(text) || text[i+11] != separator {
		return 0, false
	}
	for last := 5; last >= 4; last-- {
		if digitsAt(text, i+12, last) && cardEnds(text, i+12+last) {
			return i + 12 + last, true
		}
	}
	return 0, false
}

func passesLuhn(text string) bool {
	total, position := 0, 0
	for i := len(text) - 1; i >= 0; i-- {
		if !isDigit(text[i]) {
			continue
		}
		digit := int(text[i] - '0')
		if position%2 == 1 {
			digit *= 2
			if digit > 9 {
				digit -= 9
			}
		}
		total += digit
		position++
	}
	return total%10 == 0
}

func maskSecretShapes(text string) string {
	for _, shape := range secretShapes {
		text = maskShape(text, shape)
	}
	return text
}

func maskShape(text string, shape secretShape) string {
	var out strings.Builder
	last := 0
	for from := 0; from <= len(text); {
		found, ok := shape.find(text, from)
		if !ok {
			break
		}
		out.WriteString(text[last:found.start])
		if shape.isSecret == nil || shape.isSecret(text[found.start:found.end]) {
			out.WriteString(text[found.start:found.keepEnd] + redacted)
		} else {
			out.WriteString(text[found.start:found.end])
		}
		last, from = found.end, max(found.end, found.start+1)
	}
	if last == 0 {
		return text
	}
	out.WriteString(text[last:])
	return out.String()
}

func maskJWTs(text string) string {
	if !strings.Contains(text, "eyJ") {
		return text
	}
	return dottedRun.ReplaceAllStringFunc(text, maskJWTRun)
}

func maskJWTRun(run string) string {
	segments := strings.Split(run, ".")
	out := make([]string, 0, len(segments))
	for i := 0; i < len(segments); {
		start := strings.Index(segments[i], "eyJ")
		isJWT := start >= 0 && start < len(segments[i])-3 && i+2 < len(segments) &&
			strings.HasPrefix(segments[i+1], "eyJ") && len(segments[i+1]) > 3
		if isJWT {
			out = append(out, segments[i][:start]+redactedJWT)
			i += 3
			continue
		}
		out = append(out, segments[i])
		i++
	}
	return strings.Join(out, ".")
}

var (
	passwordLabelEndings = []string{"password", "passwd", "pwd", "secret", "secrets"}
	secretLabelEndings   = append(append([]string{}, passwordLabelEndings...), "token", "apikey", "credential", "credentials")
	keyLabelQualifiers   = []string{"secret", "private", "access", "api", "auth", "signing", "client", "session", "master", "encryption", "account", "storage"}
)

func isPasswordLabel(label string) bool {
	return hasAnySuffix(normalizedLabel(label), passwordLabelEndings)
}

// isSecretLabel holds for a label ending in a secret word, so db_password and
// AWS_SECRET_ACCESS_KEY are secret while password_hint and tokenizer_name are not. A key
// label is secret only when the word before "key" is a qualifier such as api or private.
func isSecretLabel(label string) bool {
	return hasAnySuffix(normalizedLabel(label), secretLabelEndings) || namesASecretKey(label)
}

func normalizedLabel(label string) string {
	return strings.ToLower(strings.NewReplacer("_", "", "-", "", `"`, "", "'", "").Replace(label))
}

func namesASecretKey(label string) bool {
	words := labelWords(label)
	if len(words) == 0 || !strings.HasSuffix(words[len(words)-1], "key") {
		return false
	}
	qualifier := strings.TrimSuffix(words[len(words)-1], "key")
	if qualifier == "" && len(words) > 1 {
		qualifier = words[len(words)-2]
	}
	return qualifier != "" && hasAnySuffix(qualifier, keyLabelQualifiers)
}

func labelWords(label string) []string {
	runes := []rune(label)
	var spaced strings.Builder
	for i, r := range runes {
		if i > 0 && startsCamelWord(runes, i) {
			spaced.WriteRune(' ')
		}
		spaced.WriteRune(r)
	}
	words := strings.FieldsFunc(spaced.String(), func(r rune) bool {
		return isUnicodeSpace(r) || strings.ContainsRune(`_.-"'`, r)
	})
	for i, word := range words {
		words[i] = strings.ToLower(word)
	}
	return words
}

func startsCamelWord(runes []rune, i int) bool {
	previous, current := runes[i-1], runes[i]
	if isUpper(current) && (isLower(previous) || (previous >= '0' && previous <= '9')) {
		return true
	}
	return isUpper(previous) && isUpper(current) && i+1 < len(runes) && isLower(runes[i+1])
}

func hasAnySuffix(text string, suffixes []string) bool {
	for _, suffix := range suffixes {
		if strings.HasSuffix(text, suffix) {
			return true
		}
	}
	return false
}

func digitsAt(text string, i, count int) bool {
	if i < 0 || i+count > len(text) {
		return false
	}
	for _, c := range []byte(text[i : i+count]) {
		if !isDigit(c) {
			return false
		}
	}
	return true
}

func isCardSeparator(c byte) bool { return c == ' ' || c == '-' }
func isDigit(c byte) bool         { return c >= '0' && c <= '9' }
func isLower(r rune) bool         { return r >= 'a' && r <= 'z' }
func isUpper(r rune) bool         { return r >= 'A' && r <= 'Z' }
func isASCIIAlnum(c byte) bool {
	return isDigit(c) || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}
