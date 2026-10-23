package metering

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"path"
	"reflect"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/revenium/revenium-go-sdk/core/prompt"
)

// MaxErrorReasonLength is the length of Revenium's errorReason column. A
// longer reason is accepted with a 201 and never stored.
const MaxErrorReasonLength = 255

// MaxToolErrorMessageLength caps the errorMessage of a tool event.
const MaxToolErrorMessageLength = 2048

const (
	failedCallKeyMarker   = ":err:"
	videoCompletionFailed = "FAILED"
)

// CallFailure is what the usage record of a failed call says about the
// failure: the provider's HTTP status and its error type or code. The error
// text itself is sent only on request, because providers quote API key
// fragments and the request, prompt included, in it.
type CallFailure struct {
	Err error
	// StatusCode is the HTTP status the provider answered with, 0 when no
	// answer arrived.
	StatusCode int
	// Kind is the provider's error type or code, such as "rate_limit_error".
	Kind string
	// ProviderReply marks error text that is the provider's own words, as for
	// a failed asynchronous task, even though no HTTP error status came with it.
	ProviderReply bool
}

type httpStatusCarrier interface{ HTTPStatusCode() int }

type errorCodeCarrier interface{ ErrorCode() string }

// FailureOf describes err, reading the status and kind a provider module
// recorded with ReveniumError.WithProviderAnswer or WithProviderReply, or
// those of an AWS SDK error. An error carrying a provider error code is the
// provider's own text, with or without an HTTP status.
func FailureOf(err error) CallFailure {
	failure := CallFailure{Err: err}
	var revErr *core.ReveniumError
	if errors.As(err, &revErr) {
		failure.StatusCode = revErr.StatusCode
		failure.Kind, _ = revErr.GetDetails()[core.DetailProviderErrorKind].(string)
		failure.ProviderReply, _ = revErr.GetDetails()[core.DetailProviderReply].(bool)
	}
	var withStatus httpStatusCarrier
	if failure.StatusCode == 0 && errors.As(err, &withStatus) {
		failure.StatusCode = withStatus.HTTPStatusCode()
	}
	var withCode errorCodeCarrier
	if errors.As(err, &withCode) {
		failure.ProviderReply = true
		if failure.Kind == "" {
			failure.Kind = core.ErrorKindFrom(withCode.ErrorCode())
		}
	}
	return failure
}

// ProviderFailure describes err from a provider SDK error that carries the
// HTTP status and the raw JSON body of the provider's error reply.
func ProviderFailure(err error, statusCode int, body string) CallFailure {
	return CallFailure{Err: err, StatusCode: statusCode, Kind: core.ProviderErrorKind([]byte(body))}
}

func (f CallFailure) errorCode() *int {
	if f.StatusCode < 400 || f.StatusCode > 599 {
		return nil
	}
	code := f.StatusCode
	return &code
}

func (f CallFailure) kind() string {
	if f.Kind != "" {
		return f.Kind
	}
	if f.StatusCode > 0 {
		return string(core.ErrorTypeProvider)
	}
	return errorKindOf(f.Err)
}

// sendsMessage follows REVENIUM_CAPTURE_ERROR_MESSAGES. A provider's own
// answer can quote the request, so its text also needs prompt capture on.
func (f CallFailure) sendsMessage(capturePrompts bool) bool {
	quotesProvider := f.StatusCode > 0 || f.ProviderReply
	return f.Err != nil && core.IsEnvFlagSet(core.EnvCaptureErrorMessages) && (!quotesProvider || capturePrompts)
}

func (f CallFailure) describe(capturePrompts bool, maxRunes int) string {
	suffix := ""
	if code := f.errorCode(); code != nil {
		suffix = fmt.Sprintf(" (HTTP %d)", *code)
	}
	text := f.kind()
	if f.sendsMessage(capturePrompts) {
		text += ": " + f.Err.Error()
	}
	return prompt.RedactErrorText(text, maxRunes-len(suffix)) + suffix
}

// Reason is the errorReason sent for this failure: the provider's error type
// or code, or a short name of the error, followed by " (HTTP <status>)" when
// the provider answered with an error status. With REVENIUM_CAPTURE_ERROR_MESSAGES on, the error
// text follows the kind, redacted; for an error the provider answered with
// prompt capture must be on as well. It never exceeds MaxErrorReasonLength.
func (f CallFailure) Reason(capturePrompts bool) string {
	return f.describe(capturePrompts, MaxErrorReasonLength)
}

func errorKindOf(err error) string {
	if err == nil {
		return string(core.ErrorTypeInternal)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	if errors.Is(err, context.Canceled) {
		return CallerCancelledReason
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return "timeout"
	}
	var revErr *core.ReveniumError
	if errors.As(err, &revErr) {
		return string(revErr.Type)
	}
	if errors.As(err, &netErr) {
		return string(core.ErrorTypeNetwork)
	}
	return goTypeName(err)
}

// goTypeName names the error's type the way Go code spells it, such as
// "url.Error"; the anonymous types of errors.New and fmt.Errorf are "error".
func goTypeName(err error) string {
	t := reflect.TypeOf(err)
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	pkg := t.PkgPath()
	if pkg == "errors" || pkg == "fmt" || t.Name() == "" {
		return "error"
	}
	return path.Base(pkg) + "." + t.Name()
}

// ApplyFailedCall makes payload the one usage record of a call that failed
// and then applies metadata as ApplyMetadata does, so an errorReason in the
// metadata still overrides the SDK's. Tokens already on payload, such as
// those a stream reported before it broke off, are kept. An image or video
// record gets the zero quantity its endpoint requires unless one is set, and
// a video record gets completion status FAILED.
//
// A transactionId or idempotencyKey from metadata is not reused: Revenium
// would treat the successful retry of the call, which carries the same
// metadata, as a duplicate of this record and drop it.
func ApplyFailedCall(payload *MeteringPayload, failure CallFailure, metadata map[string]interface{}) {
	payload.StopReason = string(core.StopReasonError)
	payload.ErrorReason = failure.Reason(prompt.ShouldCapturePrompts(metadata))
	payload.ErrorCode = failure.errorCode()
	markUnbilledOutcome(payload)
	applyMetadataApartFromRetry(payload, metadata)
}

// The two cancellation outcomes share the spelling of the CANCELLED stop
// reason and differ by who cancelled. CancelledReason is the errorReason of a
// call the provider cancelled, which is how a video record says so, since the
// video endpoint stores no stop reason. CallerCancelledReason is the kind of a
// call whose caller ended its context, recorded as a failed call.
const (
	CancelledReason       = "cancelled"
	CallerCancelledReason = "caller_cancelled"
)

// ApplyCancelledCall makes payload the record of a call the provider
// cancelled, with stop reason CANCELLED and error reason CancelledReason. Like
// ApplyFailedCall it zeroes an image or video record's billed quantity, gives
// a video record completion status FAILED, the closest outcome that endpoint
// stores, and applies metadata with the record's ids kept apart from a retry.
func ApplyCancelledCall(payload *MeteringPayload, metadata map[string]interface{}) {
	payload.StopReason = string(core.StopReasonCancelled)
	payload.ErrorReason = CancelledReason
	markUnbilledOutcome(payload)
	applyMetadataApartFromRetry(payload, metadata)
}

// applyMetadataApartFromRetry applies metadata but keeps the record's own
// transactionId, such as a provider task id, when metadata names the call's,
// and turns a metadata idempotencyKey into one of the record's own.
func applyMetadataApartFromRetry(payload *MeteringPayload, metadata map[string]interface{}) {
	ownTransactionID := payload.TransactionID
	ApplyMetadata(payload, metadata)
	if id, ok := metadata["transactionId"].(string); ok && id != "" && payload.TransactionID == id && ownTransactionID != id {
		payload.TransactionID = ownTransactionID
	}
	if key, ok := metadata["idempotencyKey"].(string); ok && key != "" && payload.IdempotencyKey == key {
		payload.IdempotencyKey = failedCallIdempotencyKey(key, payload.TransactionID)
	}
}

func markUnbilledOutcome(payload *MeteringPayload) {
	switch OperationType(payload.OperationType) {
	case OperationImage:
		if payload.ActualImageCount == nil {
			zero := 0
			payload.ActualImageCount = &zero
		}
	case OperationVideo:
		payload.CompletionStatus = videoCompletionFailed
		if payload.DurationSeconds == nil {
			zero := 0.0
			payload.DurationSeconds = &zero
		}
	}
}

// failedCallIdempotencyKey derives the suffix from the record's own
// transactionId, so a record reported again for the same provider operation,
// such as a Veo operation or a Runway task, gets the same key.
func failedCallIdempotencyKey(callKey, transactionID string) string {
	recordSum := sha256.Sum256([]byte(transactionID))
	suffix := hex.EncodeToString(recordSum[:4])
	key := callKey + failedCallKeyMarker + suffix
	if len(key) <= maxIdempotencyKeyLength {
		return key
	}
	sum := sha256.Sum256([]byte(callKey))
	return hex.EncodeToString(sum[:]) + failedCallKeyMarker + suffix
}

// ToolErrorMessage is the errorMessage of a tool event for a tool call that
// returned err: a short name of the error, followed by its redacted text when
// REVENIUM_CAPTURE_ERROR_MESSAGES is on (for an error that quotes a
// provider's reply, REVENIUM_CAPTURE_PROMPTS must be on as well). It never
// exceeds MaxToolErrorMessageLength.
func ToolErrorMessage(err error) string {
	return FailureOf(err).describe(prompt.ShouldCapturePrompts(nil), MaxToolErrorMessageLength)
}
