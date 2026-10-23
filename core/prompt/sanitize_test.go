package prompt

import (
	"encoding/base64"
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Each expected value is what the Python SDK's sanitize_credentials returns for the input.
func TestSanitizeCredentials_RedactsWhatThePythonSDKRedacts(t *testing.T) {
	cases := []struct {
		name, input, want string
	}{
		{"private key block", "key -----BEGIN RSA PRIVATE KEY-----\nMIIEowIBAAKCAQEA1234abcd\n-----END RSA PRIVATE KEY----- done", "key -----BEGIN RSA PRIVATE KEY-----***REDACTED*** done"},
		{"perplexity key", "use pplx-abcdefghijklmnopqrstuvwx now", "use pplx-***REDACTED*** now"},
		{"openai project key", "use sk-proj-abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWX now", "use sk-proj-***REDACTED*** now"},
		{"anthropic key", "use sk-ant-api03-abcdefghijklmnopqrstuvwx now", "use sk-ant-***REDACTED*** now"},
		{"openai key", "use sk-abcdefghijklmnopqrstuvwx now", "use sk-***REDACTED*** now"},
		{"aws access key id", "id AKIAIOSFODNN7EXAMPLE now", "id AKIA***REDACTED*** now"},
		{"github personal token", "ghp_abcdefghijklmnopqrstuvwxyzABCDEFGHIJ", "ghp_***REDACTED***"},
		{"github server token", "ghs_abcdefghijklmnopqrstuvwxyzABCDEFGHIJ", "ghs_***REDACTED***"},
		{"github oauth token", "gho_abcdefghijklmnopqrstuvwxyzABCDEFGHIJ", "gho_***REDACTED***"},
		{"github fine-grained token", "github_pat_abcdefghijklmnopqrstuvwxyzABCD", "github_pat_***REDACTED***"},
		{"google api key", "AIzaabcdefghijklmnopqrstuvwxyzABCDEFGHI", "AIza***REDACTED***"},
		{"xai key", "xai-abcdefghijklmnopqrstuvwx", "xai-***REDACTED***"},
		{"groq key", "gsk_abcdefghijklmnopqrstuvwx", "gsk_***REDACTED***"},
		{"slack token", "xoxb-" + "123456789012-abcdefghijklmnop", "xoxb-***REDACTED***"},
		{"hugging face token", "hf_" + "abcdefghijklmnopqrstuvwxyzABCDEFGH", "hf_***REDACTED***"},
		{"revenium write key", "rev_sk_abcdefghijklmnopqrst", "rev_sk_***REDACTED***"},
		{"revenium metering key", "rev_mk_abcdefghijklmnopqrst", "rev_mk_***REDACTED***"},
		{"revenium hosted key", "hak_abcdefghijklmnopqrst", "hak_***REDACTED***"},
		{"fal key", "FAL_KEY 0f1e2d3c-4b5a-6978-8a9b-0c1d2e3f4a5b:0123456789abcdef0123456789abcdef", "FAL_KEY 0f1e2d3c-4b5a-6978-8a9b-0c1d2e3f4a5b:***REDACTED***"},
		{"card number with expiry", "card 4111 1111 1111 1111 12 exp", "card ***REDACTED*** 12 exp"},
		{"card number", "card 4111111111111111 please", "card ***REDACTED*** please"},
		{"json web token", "jwt eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.c2lnbmF0dXJl end", "jwt ***REDACTED_JWT*** end"},
		{"short bearer token", "Authorization: Bearer abc123def456", "Authorization: Bearer ***REDACTED***"},
		{"basic credential", "Authorization: Basic YWRtaW46aHVudGVyMjI=", "Authorization: Basic ***REDACTED***"},
		{"signed url", "https://bucket.s3.amazonaws.com/f.png?X-Amz-Signature=abcdef0123456789&x=1", "https://bucket.s3.amazonaws.com/f.png?X-Amz-Signature=***REDACTED***&x=1"},
		{"password in a url", "postgres://admin:hunter22@db.example.com/app", "postgres://admin:***REDACTED***@db.example.com/app"},
		{"quoted password label", "config {\"db_password\": \"short123\"} end", "config {\"db_password\": \"***REDACTED***\"} end"},
		{"secret access key label", "AWS_SECRET_ACCESS_KEY=wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY", "AWS_SECRET_ACCESS_KEY=***REDACTED***"},
		{"credentials label", "github_credentials: abcdefghijklmnopqrstuvwx", "github_credentials: ***REDACTED***"},
		{"secret key in json", "{\"client_secret\": \"s3cr3tvalue\", \"note\": \"ok\"}", "{\"client_secret\": \"***REDACTED***\", \"note\": \"ok\"}"},
		{"password hint stays", "password_hint: my first pet", "password_hint: my first pet"},
		{"tokenizer name stays", "tokenizer_name: abcdefghijklmnopqrstuvwx", "tokenizer_name: abcdefghijklmnopqrstuvwx"},
		{"bearer in prose stays", "Bearer of good news", "Bearer of good news"},
		{"key inside a word stays", "mysk-abcdefghijklmnopqrstuvwx", "mysk-abcdefghijklmnopqrstuvwx"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := SanitizeCredentials(tc.input)
			if json.Valid([]byte(tc.want)) {
				require.JSONEq(t, tc.want, got)
				return
			}
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestSanitizeCredentials_ReturnsJSONWithNothingToRedactUnchanged(t *testing.T) {
	text := `{"note": "ok",   "count": 3}`

	assert.Equal(t, text, SanitizeCredentials(text))
}

func TestSanitizeCredentials_KeepsALuhnFailingNumber(t *testing.T) {
	assert.Equal(t, "order 4111111111111112 shipped", SanitizeCredentials("order 4111111111111112 shipped"))
}

func TestSanitizeCredentials_MasksSecretShapesInsideSerializedEscapes(t *testing.T) {
	got := SanitizeCredentials(`"line one\nsk-ant-abcdefghijklmnopqrstuvwx"`)

	assert.Equal(t, `"line one\nsk-ant-***REDACTED***"`, got)
}

func TestSanitizeCredentials_MasksASecretNamedNumber(t *testing.T) {
	assert.JSONEq(t, `{"password":"***REDACTED***","port":5432}`, SanitizeCredentials(`{"password": 12345678, "port": 5432}`))
}

func TestSanitizeCredentials_KeepsTextAfterAJSONValue(t *testing.T) {
	text := "{\"answer\": 1}\nExplanation with sk-ant-abcdefghijklmnopqrstuvwx"

	assert.Equal(t, "{\"answer\": 1}\nExplanation with sk-ant-***REDACTED***", SanitizeCredentials(text))
}

// Linear work grows by linearityGrowth when the input does, quadratic work by its square (64).
// Measured under -race from a 4 KB base: linear scans 8.8 to 11.7 on Go 1.26 and up to 19.3 on
// Go 1.21 (CI's minimum), whose regexp clears a bitmap sized to the rest of the input on every
// call in a plain instrumented loop; the pre-fix prefix scan 189 to 225, past the safety
// ceiling. A 2 KB base with a limit of 16 failed on Go 1.21 CI at 16.4, and a limit of 32
// failed on the Go 1.21 job behind the CI egress sink at 35.5, so the limit sits at 6x the
// growth: still well under the quadratic 64, with the fastest of five runs per size.
const (
	linearityBaseBytes     = 4 << 10
	linearityGrowth        = 8
	maxLinearityRatio      = 6 * linearityGrowth
	linearityTimedRuns     = 5
	linearitySafetyCeiling = 30 * time.Second
)

func TestSanitizeCredentials_StaysLinearOnNearMissText(t *testing.T) {
	units := map[string]string{
		"near-miss key prefixes":  "ask-",
		"near-miss github tokens": "xghp_",
		"near-miss google keys":   "aAIza",
		"near-miss revenium keys": "xhak_",
		"near-miss slack tokens":  "1xoxb-123456",
		"digit run":               "4",
		"grouped digits":          "4111 ",
		"labels without values":   "password={",
		"basic headers":           "xBasic aBasic ",
		"pem headers":             "-----BEGIN PRIVATE KEY-----",
		"dotted runs":             "eyJa.",
	}
	lowest, highest := math.Inf(1), math.Inf(-1)
	for name, unit := range units {
		t.Run(name, func(t *testing.T) {
			small := strings.Repeat(unit, linearityBaseBytes/len(unit))
			large := strings.Repeat(small, linearityGrowth)

			ratio := sanitizeGrowthRatio(t, small, large)

			t.Logf("%d to %d bytes grew the time %.2fx", len(small), len(large), ratio)
			lowest, highest = math.Min(lowest, ratio), math.Max(highest, ratio)
			assert.Less(t, ratio, float64(maxLinearityRatio))
		})
	}
	t.Logf("growth ratios ranged from %.2f to %.2f", lowest, highest)
}

// sanitizeGrowthRatio alternates small and large runs so a slow stretch on a shared runner
// hits both sides, and keeps the fastest of each.
func sanitizeGrowthRatio(t *testing.T, small, large string) float64 {
	SanitizeCredentials(small)
	fastestSmall, fastestLarge := time.Duration(math.MaxInt64), time.Duration(math.MaxInt64)
	for run := 0; run < linearityTimedRuns; run++ {
		fastestSmall = min(fastestSmall, timeSanitize(small))
		largeTime := timeSanitize(large)
		require.Less(t, largeTime, linearitySafetyCeiling)
		fastestLarge = min(fastestLarge, largeTime)
	}
	return float64(fastestLarge) / float64(fastestSmall)
}

func timeSanitize(text string) time.Duration {
	started := time.Now()
	SanitizeCredentials(text)
	return time.Since(started)
}

func TestSanitizeCredentials_MasksACardNumberStoredAsAJSONNumber(t *testing.T) {
	assert.JSONEq(t, `{"card":"***REDACTED***","qty":2}`, SanitizeCredentials(`{"card": 4111111111111111, "qty": 2}`))
	assert.JSONEq(t, `["***REDACTED***"]`, SanitizeCredentials(`[4111111111111111]`))
}

func TestSanitizeCredentials_MasksATokenOrKeySeparatedOnlyBySpace(t *testing.T) {
	assert.Equal(t, "Authorization: token ***REDACTED***", SanitizeCredentials("Authorization: token 0123456789abcdef0123456789abcdef01234567"))
	assert.Equal(t, "apikey ***REDACTED***", SanitizeCredentials("apikey abcdefghij1234567890extra"))
	assert.Equal(t, "password reset successful", SanitizeCredentials("password reset successful"))
	assert.Equal(t, "the access token expired", SanitizeCredentials("the access token expired"))
}

func TestSanitizeCutText_MasksABasicCredentialCutAtTheEnd(t *testing.T) {
	credential := base64.StdEncoding.EncodeToString([]byte("admin:" + strings.Repeat("p", 60)))
	cut := "Authorization: Basic " + credential[:len(credential)-3]

	assert.Equal(t, cut, SanitizeCredentials(cut), "a complete text keeps the strict user:password check")
	assert.Equal(t, "Authorization: Basic ***REDACTED***", SanitizeCutText(cut))
}

func TestSanitizeCutText_LeavesTextWithoutACutCredentialAsSanitizeCredentialsDoes(t *testing.T) {
	cases := map[string]string{
		"short word after Basic":          "Use Basic auth",
		"Basic inside a word":             "xBasic YWRtaW46aHVudGVy",
		"credential not at the cut":       "Basic YWRtaW46aHVudGVyMjI= then more",
		"cut credential already followed": "Basic YWRtaW46aHVudGVyMj and more",
	}
	for name, text := range cases {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, SanitizeCredentials(text), SanitizeCutText(text))
		})
	}
}

func TestSanitizeCutText_MasksABasicCredentialCutAtTheEndOfAnyJSONString(t *testing.T) {
	credential := base64.StdEncoding.EncodeToString([]byte("admin:" + strings.Repeat("p", 60)))
	cut := credential[:len(credential)-3]
	cases := map[string]struct{ input, want string }{
		"string value":        {`{"input":"Basic ` + cut + `"}`, `{"input":"Basic ***REDACTED***"}`},
		"value before others": {`{"input":"Basic ` + cut + `","n":1}`, `{"input":"Basic ***REDACTED***","n":1}`},
		"nested in a list":    {`[{"args":["Basic ` + cut + `"]}]`, `[{"args":["Basic ***REDACTED***"]}]`},
		"cut tool input text": {`{"input":"{\"note\":\"Basic ` + cut + `"}`, `{"input":"{\"note\":\"Basic ***REDACTED***"}`},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tc.input, SanitizeCredentials(tc.input), "a complete text keeps the strict user:password check")
			assert.JSONEq(t, tc.want, SanitizeCutText(tc.input))
		})
	}
}

// Go code pasted into a prompt assigns with ":=", and other languages use "=>" or "==";
// the Python SDK accepts one ':' or '=' only.
func TestSanitizeCredentials_MasksAValueAfterASeparatorRun(t *testing.T) {
	cases := map[string]struct{ input, want string }{
		"go api key":   {`apiKey := "a1b2c3d4e5f6g7h8i9j0k1l2m3"`, `apiKey := "***REDACTED***"`},
		"go token":     {`token := "a1b2c3d4e5f6g7h8i9j0k1l2m3"`, `token := "***REDACTED***"`},
		"go password":  {`password := "hunter2222xyz"`, `password := "***REDACTED***"`},
		"arrow key":    {`api_key => "a1b2c3d4e5f6g7h8i9j0k1l2m3"`, `api_key => "***REDACTED***"`},
		"double equal": {`token=="a1b2c3d4e5f6g7h8i9j0k1l2m3"`, `token=="***REDACTED***"`},
		"password starting with the second equal": {`DB_PASSWORD==hunter2`, `DB_PASSWORD=***REDACTED***`},
		"long password starting with an equal":    {`DB_PASSWORD==hunter22`, `DB_PASSWORD=***REDACTED***`},
		"token starting with the second equal":    {`token==a1b2c3d4e5f6g7h8i9j`, `token=***REDACTED***`},
		"too short either way":                    {`password=:x`, `password=:x`},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tc.want, SanitizeCredentials(tc.input))
		})
	}
}

func TestSanitizeCredentials_TreatsJSONFollowedByMoreJSONAsText(t *testing.T) {
	text := `{"note": "ok"} {"password": "hunter2222xyz"}`

	assert.Equal(t, `{"note": "ok"} {"password": "***REDACTED***"}`, SanitizeCredentials(text))
}

func TestSanitizeCredentials_TreatsUnicodeSpacesAsPythonDoes(t *testing.T) {
	cases := map[string]struct{ input, want string }{
		"bearer after a non-breaking space":  {"Authorization: Bearer abc123def456", "Authorization: Bearer ***REDACTED***"},
		"basic after an ideographic space":   {"Authorization: Basic　YWRtaW46aHVudGVyMjI=", "Authorization: Basic　***REDACTED***"},
		"bearer after a vertical tab":        {"Bearer\vabc123def456", "Bearer\v***REDACTED***"},
		"signed url ends at a unicode space": {"https://x.io/f?sig=abcdef0123 next", "https://x.io/f?sig=***REDACTED*** next"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tc.want, SanitizeCredentials(tc.input))
		})
	}
}
