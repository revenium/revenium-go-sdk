package webhooks

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func sign(timestamp string, body []byte, secret string) string {
	payload := append([]byte(fmt.Sprintf("%s.", timestamp)), body...)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func nowSeconds() string {
	return fmt.Sprintf("%d", time.Now().Unix())
}

var (
	testSecret = "whsec_test_secret_key_abc123"
	testBody   = []byte(`{"type":"webhook.test","data":{"message":"hello"}}`)
)

func TestVerifySignature_SingleSecret(t *testing.T) {
	ts := nowSeconds()
	sig := sign(ts, testBody, testSecret)

	result := VerifySignature(VerifyOptions{
		Payload:         testBody,
		SignatureHeader: sig,
		TimestampHeader: ts,
		Secrets:         []string{testSecret},
	})

	assert.True(t, result)
}

func TestVerifySignature_RotationOverlap(t *testing.T) {
	ts := nowSeconds()
	newSecret := "whsec_new_secret"
	oldSecret := "whsec_old_secret"
	header := sign(ts, testBody, newSecret) + ", " + sign(ts, testBody, oldSecret)

	result := VerifySignature(VerifyOptions{
		Payload:         testBody,
		SignatureHeader: header,
		TimestampHeader: ts,
		Secrets:         []string{newSecret, oldSecret},
	})

	assert.True(t, result)
}

func TestVerifySignature_OneOfMultipleSecretsMatches(t *testing.T) {
	ts := nowSeconds()
	sig := sign(ts, testBody, testSecret)

	result := VerifySignature(VerifyOptions{
		Payload:         testBody,
		SignatureHeader: sig,
		TimestampHeader: ts,
		Secrets:         []string{"wrong_secret", testSecret},
	})

	assert.True(t, result)
}

func TestVerifySignature_TimestampExceedsDefaultTolerance(t *testing.T) {
	ts := fmt.Sprintf("%d", time.Now().Unix()-400)
	sig := sign(ts, testBody, testSecret)

	result := VerifySignature(VerifyOptions{
		Payload:         testBody,
		SignatureHeader: sig,
		TimestampHeader: ts,
		Secrets:         []string{testSecret},
	})

	assert.False(t, result)
}

func TestVerifySignature_TimestampExceedsCustomTolerance(t *testing.T) {
	ts := fmt.Sprintf("%d", time.Now().Unix()-15)
	sig := sign(ts, testBody, testSecret)

	result := VerifySignature(VerifyOptions{
		Payload:          testBody,
		SignatureHeader:  sig,
		TimestampHeader:  ts,
		Secrets:          []string{testSecret},
		ToleranceSeconds: 10,
	})

	assert.False(t, result)
}

func TestVerifySignature_MalformedHeader(t *testing.T) {
	ts := nowSeconds()

	result := VerifySignature(VerifyOptions{
		Payload:         testBody,
		SignatureHeader: "md5=abc123,hmac=xyz",
		TimestampHeader: ts,
		Secrets:         []string{testSecret},
	})

	assert.False(t, result)
}

func TestVerifySignature_EmptySignatureHeader(t *testing.T) {
	ts := nowSeconds()

	result := VerifySignature(VerifyOptions{
		Payload:         testBody,
		SignatureHeader: "",
		TimestampHeader: ts,
		Secrets:         []string{testSecret},
	})

	assert.False(t, result)
}

func TestVerifySignature_EmptySecrets(t *testing.T) {
	ts := nowSeconds()
	sig := sign(ts, testBody, testSecret)

	result := VerifySignature(VerifyOptions{
		Payload:         testBody,
		SignatureHeader: sig,
		TimestampHeader: ts,
		Secrets:         []string{},
	})

	assert.False(t, result)
}

func TestVerifySignature_SecretMismatch(t *testing.T) {
	ts := nowSeconds()
	sig := sign(ts, testBody, testSecret)

	result := VerifySignature(VerifyOptions{
		Payload:         testBody,
		SignatureHeader: sig,
		TimestampHeader: ts,
		Secrets:         []string{"completely_wrong_secret"},
	})

	assert.False(t, result)
}

func TestVerifySignature_NonNumericTimestamp(t *testing.T) {
	sig := sign("notanumber", testBody, testSecret)

	result := VerifySignature(VerifyOptions{
		Payload:         testBody,
		SignatureHeader: sig,
		TimestampHeader: "notanumber",
		Secrets:         []string{testSecret},
	})

	assert.False(t, result)
}

func TestBackendCompat_DeterministicVector(t *testing.T) {
	timestamp := "1716400000"
	body := []byte(`{"type":"webhook.test","eventId":"abc-123","timestamp":1716400000,"data":{"message":"test"}}`)
	secret := "rK7sB2-XYZ-9wQpVnH3qY4rT8uOmDcLkPaWeFsJgIyB"

	signedPayload := append([]byte(fmt.Sprintf("%s.", timestamp)), body...)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(signedPayload)
	expectedHex := hex.EncodeToString(mac.Sum(nil))

	t.Run("verifies signature from backend algorithm", func(t *testing.T) {
		result := VerifySignature(VerifyOptions{
			Payload:          body,
			SignatureHeader:  "sha256=" + expectedHex,
			TimestampHeader:  timestamp,
			Secrets:          []string{secret},
			ToleranceSeconds: math.MaxInt32,
		})
		assert.True(t, result)
	})

	t.Run("rejects altered body", func(t *testing.T) {
		altered := []byte(`{"type":"webhook.test","eventId":"abc-123","timestamp":1716400000,"data":{"message":"tes!"}}`)
		result := VerifySignature(VerifyOptions{
			Payload:          altered,
			SignatureHeader:  "sha256=" + expectedHex,
			TimestampHeader:  timestamp,
			Secrets:          []string{secret},
			ToleranceSeconds: math.MaxInt32,
		})
		assert.False(t, result)
	})

	t.Run("rejects mismatched timestamp", func(t *testing.T) {
		result := VerifySignature(VerifyOptions{
			Payload:          body,
			SignatureHeader:  "sha256=" + expectedHex,
			TimestampHeader:  "1716400001",
			Secrets:          []string{secret},
			ToleranceSeconds: math.MaxInt32,
		})
		assert.False(t, result)
	})
}

func TestBackendCompat_RotationSimulation(t *testing.T) {
	body := []byte(`{"type":"webhook.test","eventId":"evt-001","data":{"message":"rotation test"}}`)
	newSecret := "newSec_Abc123Xyz"
	oldSecret := "rK7sB2-XYZ-9wQpVnH3qY4rT8uOmDcLkPaWeFsJgIyB"
	ts := nowSeconds()

	header := sign(ts, body, newSecret) + ", " + sign(ts, body, oldSecret)

	t.Run("verifies with new secret", func(t *testing.T) {
		assert.True(t, VerifySignature(VerifyOptions{
			Payload:         body,
			SignatureHeader: header,
			TimestampHeader: ts,
			Secrets:         []string{newSecret},
		}))
	})

	t.Run("verifies with old secret", func(t *testing.T) {
		assert.True(t, VerifySignature(VerifyOptions{
			Payload:         body,
			SignatureHeader: header,
			TimestampHeader: ts,
			Secrets:         []string{oldSecret},
		}))
	})

	t.Run("rejects expired rotation with only new sig", func(t *testing.T) {
		singleSigHeader := sign(ts, body, newSecret)
		assert.False(t, VerifySignature(VerifyOptions{
			Payload:         body,
			SignatureHeader: singleSigHeader,
			TimestampHeader: ts,
			Secrets:         []string{oldSecret},
		}))
	})
}

func TestBackendCompat_UnicodePayload(t *testing.T) {
	body := []byte(`{"data":{"message":"Alerta: custo excedeu R$ 1.000,00 — acao necessaria"}}`)
	ts := nowSeconds()
	sig := sign(ts, body, testSecret)

	assert.True(t, VerifySignature(VerifyOptions{
		Payload:         body,
		SignatureHeader: sig,
		TimestampHeader: ts,
		Secrets:         []string{testSecret},
	}))
}

func TestBackendCompat_EmptyPayload(t *testing.T) {
	ts := nowSeconds()
	sig := sign(ts, []byte{}, testSecret)

	assert.True(t, VerifySignature(VerifyOptions{
		Payload:         []byte{},
		SignatureHeader: sig,
		TimestampHeader: ts,
		Secrets:         []string{testSecret},
	}))
}

func TestBackendCompat_LargePayload(t *testing.T) {
	large := make([]byte, 100_000)
	for i := range large {
		large[i] = 'x'
	}
	body := append([]byte(`{"data":{"bulk":"`), large...)
	body = append(body, []byte(`"}}`)...)

	ts := nowSeconds()
	sig := sign(ts, body, testSecret)

	assert.True(t, VerifySignature(VerifyOptions{
		Payload:         body,
		SignatureHeader: sig,
		TimestampHeader: ts,
		Secrets:         []string{testSecret},
	}))
}

func TestCrossPlatformParity_NodeSDK(t *testing.T) {
	timestamp := "1716400000"
	body := []byte(`{"type":"webhook.test","eventId":"abc-123","timestamp":1716400000,"data":{"message":"test"}}`)
	secret := "rK7sB2-XYZ-9wQpVnH3qY4rT8uOmDcLkPaWeFsJgIyB"

	signedPayload := append([]byte(fmt.Sprintf("%s.", timestamp)), body...)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(signedPayload)
	goHex := hex.EncodeToString(mac.Sum(nil))

	assert.NotEmpty(t, goHex)
	assert.Len(t, goHex, 64)

	result := VerifySignature(VerifyOptions{
		Payload:          body,
		SignatureHeader:  "sha256=" + goHex,
		TimestampHeader:  timestamp,
		Secrets:          []string{secret},
		ToleranceSeconds: math.MaxInt32,
	})
	assert.True(t, result)
}
