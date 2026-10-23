package metering

import (
	"math"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/revenium/revenium-go-sdk/core"
)

// The platform rejects the whole record with a 400 when one of these values
// is out of bounds (AICompletionMetadataResource ticketId @Size(max = 256)
// and retryNumber a 32-bit Int, IdempotencyKeyFilter accepts 1-255
// characters in 0x21-0x7E, printable ASCII without space), so the SDK fixes
// or drops the one value and keeps the record. ticketId is counted in UTF-16
// units because that is how the platform's Java validator measures it.
const (
	maxTicketIDUnits                  = 256
	maxRetryNumber                    = math.MaxInt32
	maxIdempotencyKeyLength           = 255
	firstSendableIdempotencyByte byte = 0x21
	lastSendableIdempotencyByte  byte = 0x7e
)

var metadataStringGuards = map[string]func(value, source string) string{
	"agenticJobId":   sanitizeAgenticJobID,
	"ticketId":       func(s, _ string) string { return truncateTicketID(s) },
	"idempotencyKey": func(s, _ string) string { return dropUnsendableIdempotencyKey(s) },
}

func guardMetadataString(key, value string) string {
	return guardFieldValue(key, value, "request metadata")
}

func guardFieldValue(key, value, source string) string {
	if guard, ok := metadataStringGuards[key]; ok {
		return guard(value, source)
	}
	return value
}

func truncateErrorReason(reason string) string {
	if utf8.RuneCountInString(reason) <= MaxErrorReasonLength {
		return reason
	}
	core.Warn("[METERING] errorReason is longer than the %d characters Revenium stores; sending its first %d", MaxErrorReasonLength, MaxErrorReasonLength)
	return string([]rune(reason)[:MaxErrorReasonLength])
}

func truncateTicketID(ticketID string) string {
	return truncateUTF16("ticketId", ticketID, maxTicketIDUnits)
}

func truncateUTF16(key, value string, maxUnits int) string {
	units := 0
	for i, r := range value {
		units += len(utf16.Encode([]rune{r}))
		if units > maxUnits {
			core.Warn("[METERING] %s is longer than %d characters; sending its first %d", key, maxUnits, maxUnits)
			return value[:i]
		}
	}
	return value
}

func isSendableIdempotencyKey(key string) bool {
	if key == "" || len(key) > maxIdempotencyKeyLength {
		return false
	}
	for i := 0; i < len(key); i++ {
		if key[i] < firstSendableIdempotencyByte || key[i] > lastSendableIdempotencyByte {
			return false
		}
	}
	return true
}

func dropUnsendableIdempotencyKey(key string) string {
	if isSendableIdempotencyKey(key) {
		return key
	}
	core.Warn("[METERING] ignoring the idempotencyKey from request metadata: it must be 1 to %d printable ASCII characters without spaces; the record keeps its generated key", maxIdempotencyKeyLength)
	return ""
}

func ensureSendableIdempotencyKey(key *string) {
	if isSendableIdempotencyKey(*key) {
		return
	}
	if *key != "" {
		core.Warn("[METERING] replacing an idempotency key that is not 1 to %d printable ASCII characters without spaces with a generated one, so the record can be sent", maxIdempotencyKeyLength)
	}
	*key = GenerateTransactionID()
}

func isFinite(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}

func warnDroppedMetadataNumber(key string, value float64) {
	core.Warn("[METERING] dropping %s from request metadata: %v is not a finite number; the record is sent without it", key, value)
}

// retryNumberFromMetadata reads a retryNumber given as an int or a float64,
// cutting a fraction off as before; ok is false when value is neither or is
// not a count the platform accepts, in which case a warning names it.
func retryNumberFromMetadata(value interface{}) (retry int, ok bool) {
	switch v := value.(type) {
	case int:
		retry = v
	case float64:
		if !isFinite(v) || v < 0 || v >= maxRetryNumber+1 {
			warnDroppedRetryNumber(v, "request metadata")
			return 0, false
		}
		retry = int(v)
	default:
		return 0, false
	}
	if !isSendableRetryNumber(retry) {
		warnDroppedRetryNumber(retry, "request metadata")
		return 0, false
	}
	return retry, true
}

func isSendableRetryNumber(retry int) bool {
	return retry >= 0 && int64(retry) <= maxRetryNumber
}

// dropUnsendableRetryNumber covers a retryNumber set on the record directly
// or taken from the environment, which ApplyMetadata never saw.
func dropUnsendableRetryNumber(payload *MeteringPayload) {
	if payload.RetryNumber == nil || isSendableRetryNumber(*payload.RetryNumber) {
		return
	}
	warnDroppedRetryNumber(*payload.RetryNumber, "the usage record")
	payload.RetryNumber = nil
}

func warnDroppedRetryNumber(value interface{}, source string) {
	core.Warn("[METERING] dropping %s from %s: %v is not a whole number from 0 to %d; the record is sent without it", retryNumberKey, source, value, maxRetryNumber)
}
