package prompt

import (
	"regexp"
)

const (
	errorTextRedacted   = "[REDACTED]"
	maxLoggedErrorRunes = 2048
)

// Applied in order after SanitizeCredentials. RE2 has no lookbehind, so a
// pattern that must start at a word boundary captures the character before
// it and puts it back.
var errorTextPatterns = []struct {
	regex       *regexp.Regexp
	replacement string
}{
	{regexp.MustCompile(`(^|[^A-Za-z0-9])(?:sk-|pk-|rk-|pplx-|xai-|gsk_|rev_sk_|rev_mk_|hak_|AIza|hf_|github_pat_|gh[oprsu]_)[^\s'",;&]*`), "${1}" + errorTextRedacted},
	{regexp.MustCompile(`(?i)(^|[^A-Za-z0-9])(Bearer|Basic|Token)\s+[^\s'",]+`), "${1}${2} " + errorTextRedacted},
	{regexp.MustCompile(`(://)[^/\s@]+@`), "${1}" + errorTextRedacted + "@"},
	{regexp.MustCompile(`([?&][^=&#\s'"]+=)[^&#\s'"]*`), "${1}" + errorTextRedacted},
	{regexp.MustCompile(`(?i)((?:api[_-]?key|key|secret|token|password|passwd|pwd|credentials?|authorization|auth|signature|hash)["']?\s*[:=]\s*)(?:"[^"]*"|'[^']*'|[^\s,;&'"}]+)`), "${1}" + errorTextRedacted},
	{regexp.MustCompile(`(^|[^0-9A-Za-z])[0-9A-Fa-f]{32,}`), "${1}" + errorTextRedacted},
	{regexp.MustCompile(`[^\s@'",;:<>()/\[\]]+@[^\s@'",;:<>()]+\.[^\s@'",;:<>()]+`), errorTextRedacted},
}

// RedactErrorText removes credentials and personal data from error text and
// cuts it to maxRunes characters. Vendor key shapes and words that start like
// a key (masked key tails included), JSON Web Tokens, bearer and basic
// credentials, URL user info, query-string values, values of key, secret,
// token, password and hash fields, long hex runs such as key hashes, and
// email addresses are each replaced with "[REDACTED]". Redaction runs before
// the cut, so a cut never leaves part of a secret behind.
func RedactErrorText(text string, maxRunes int) string {
	redacted := SanitizeCredentials(text)
	for _, p := range errorTextPatterns {
		redacted = p.regex.ReplaceAllString(redacted, p.replacement)
	}
	return truncateRunes(redacted, maxRunes)
}

// RedactedError is err's text with the redactions of RedactErrorText, for a
// log line shown at the default level.
func RedactedError(err error) string {
	return RedactErrorText(err.Error(), maxLoggedErrorRunes)
}

func truncateRunes(text string, maxRunes int) string {
	if maxRunes <= 0 {
		return ""
	}
	count := 0
	for i := range text {
		if count == maxRunes {
			return text[:i]
		}
		count++
	}
	return text
}
