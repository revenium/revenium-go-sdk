package webhooks

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

const (
	defaultToleranceSeconds = 300
	signaturePrefix         = "sha256="
)

type VerifyOptions struct {
	Payload          []byte
	SignatureHeader  string
	TimestampHeader  string
	Secrets          []string
	ToleranceSeconds int
}

func VerifySignature(opts VerifyOptions) bool {
	tolerance := opts.ToleranceSeconds
	if tolerance == 0 {
		tolerance = defaultToleranceSeconds
	}

	if len(opts.Secrets) == 0 {
		return false
	}

	signatures := parseSignatures(opts.SignatureHeader)
	if len(signatures) == 0 {
		return false
	}

	timestamp, err := strconv.ParseInt(opts.TimestampHeader, 10, 64)
	if err != nil {
		return false
	}

	diff := math.Abs(float64(time.Now().Unix() - timestamp))
	if diff > float64(tolerance) {
		return false
	}

	signedPayload := append([]byte(fmt.Sprintf("%s.", opts.TimestampHeader)), opts.Payload...)

	for _, secret := range opts.Secrets {
		expected := computeHmac(secret, signedPayload)
		for _, sig := range signatures {
			if hmac.Equal([]byte(expected), []byte(sig)) {
				return true
			}
		}
	}

	return false
}

func parseSignatures(header string) []string {
	parts := strings.Split(header, ",")
	var signatures []string
	for _, p := range parts {
		trimmed := strings.TrimSpace(p)
		if strings.HasPrefix(trimmed, signaturePrefix) {
			signatures = append(signatures, trimmed[len(signaturePrefix):])
		}
	}
	return signatures
}

func computeHmac(secret string, payload []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	return hex.EncodeToString(mac.Sum(nil))
}
