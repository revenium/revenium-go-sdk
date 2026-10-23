package metering

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/revenium/revenium-go-sdk/core/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	openAIRevokedKeyBody = `{"message":"Incorrect API key provided: sk-proj-********************FAKEWXYZ. You can find your API key at https://platform.openai.com/account/api-keys.","type":"invalid_request_error","param":null,"code":"invalid_api_key"}`
	liteLLMRejectedKey   = `{"error":{"message":"Authentication Error, Invalid proxy server token passed. Received API Key = sk-...FAKE1234, Key Hash (Token) =0f1e2d3c4b5a69788796a5b4c3d2e1f00f1e2d3c4b5a69788796a5b4c3d2e1f0. Unable to find token in cache or LiteLLM_VerificationTokenTable","type":"auth_error","param":"None","code":"401"}}`
	liteLLMEchoedPrompt  = `{"error":{"message":"litellm.BadRequestError: Invalid request body: {'model': 'gpt-4.1-nano', 'messages': [{'role': 'user', 'content': 'say hello please'}], 'temperatur': 2}","type":"invalid_request_error","param":null,"code":"400"}}`
)

var sensitiveErrorParts = []string{"FAKEWXYZ", "sk-proj", "FAKE1234", "0f1e2d3c4b5a6978", "say hello please", "Incorrect API key", "Invalid request body"}

func providerAnswer(status int, body string) error {
	return core.NewProviderError(fmt.Sprintf("LiteLLM proxy returned %d: %s", status, body), nil).WithProviderAnswer(status, []byte(body))
}

func failedChatRecord(failure CallFailure, metadata map[string]interface{}) *MeteringPayload {
	payload := NewPayload(OperationChat, "gpt-4.1-nano", "OPENAI").Build()
	ApplyFailedCall(payload, failure, metadata)
	return payload
}

func TestApplyFailedCall_SendsTheStatusAndErrorTypeButNoProviderText(t *testing.T) {
	cases := map[string]struct {
		failure CallFailure
		reason  string
		code    int
	}{
		"revoked openai key":     {ProviderFailure(errors.New("POST ...: 401 Unauthorized "+openAIRevokedKeyBody), 401, openAIRevokedKeyBody), "invalid_request_error/invalid_api_key (HTTP 401)", 401},
		"litellm rejected key":   {FailureOf(providerAnswer(401, liteLLMRejectedKey)), "auth_error (HTTP 401)", 401},
		"litellm echoed request": {FailureOf(providerAnswer(400, liteLLMEchoedPrompt)), "invalid_request_error (HTTP 400)", 400},
	}
	t.Setenv(core.EnvCapturePrompts, "")
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			for _, captureMessages := range []string{"", "true"} {
				t.Setenv(core.EnvCaptureErrorMessages, captureMessages)
				payload := failedChatRecord(tc.failure, nil)

				assert.Equal(t, "ERROR", payload.StopReason)
				assert.Equal(t, tc.reason, payload.ErrorReason)
				require.NotNil(t, payload.ErrorCode)
				assert.Equal(t, tc.code, *payload.ErrorCode)
				for _, part := range sensitiveErrorParts {
					assert.NotContains(t, payload.ErrorReason, part)
				}
			}
		})
	}
}

func TestApplyFailedCall_SendsRedactedProviderTextOnlyWithErrorMessagesAndPromptCaptureOn(t *testing.T) {
	t.Setenv(core.EnvCaptureErrorMessages, "true")
	payload := failedChatRecord(FailureOf(providerAnswer(401, liteLLMRejectedKey)), map[string]interface{}{"capturePrompts": true})

	assert.True(t, strings.HasPrefix(payload.ErrorReason, "auth_error: [PROVIDER_ERROR] LiteLLM proxy returned 401"), payload.ErrorReason)
	assert.True(t, strings.HasSuffix(payload.ErrorReason, " (HTTP 401)"), "the cut keeps the status: %s", payload.ErrorReason)
	assert.LessOrEqual(t, utf8.RuneCountInString(payload.ErrorReason), MaxErrorReasonLength)
	assert.NotContains(t, payload.ErrorReason, "FAKE1234")
	assert.NotContains(t, payload.ErrorReason, "0f1e2d3c4b5a6978")
}

func TestApplyFailedCall_TransportFailureKeepsCredentialsInTheBaseURLOut(t *testing.T) {
	t.Setenv(core.EnvCapturePrompts, "")
	transport := &url.Error{Op: "Post", URL: "https://admin:hunter2@proxy.internal/v1/chat/completions?key=AIzaFAKE", Err: errors.New("connection refused")}
	failure := FailureOf(core.NewNetworkError("request to LiteLLM proxy failed", transport))

	t.Setenv(core.EnvCaptureErrorMessages, "")
	assert.Equal(t, "NETWORK_ERROR", failedChatRecord(failure, nil).ErrorReason)
	assert.Nil(t, failedChatRecord(failure, nil).ErrorCode, "no HTTP answer, no error code")

	t.Setenv(core.EnvCaptureErrorMessages, "true")
	reason := failedChatRecord(failure, nil).ErrorReason
	assert.Contains(t, reason, "connection refused")
	assert.NotContains(t, reason, "hunter2")
	assert.NotContains(t, reason, "admin")
	assert.NotContains(t, reason, "AIzaFAKE")
}

type awsLikeError struct{}

func (awsLikeError) Error() string            { return "ThrottlingException: Too many tokens, please wait" }
func (awsLikeError) ErrorCode() string        { return "ThrottlingException" }
func (awsLikeError) HTTPStatusCode() int      { return 429 }
func (awsLikeError) ErrorMessage() string     { return "Too many tokens, please wait" }
func (awsLikeError) ErrorFault() fmt.Stringer { return nil }

func TestFailureOf_NamesErrorsWithoutAProviderAnswer(t *testing.T) {
	cases := map[string]struct {
		err    error
		reason string
	}{
		"aws sdk error":       {fmt.Errorf("bedrock api error: %w", awsLikeError{}), "ThrottlingException (HTTP 429)"},
		"deadline":            {core.NewNetworkError("request failed", context.DeadlineExceeded), "timeout"},
		"caller cancelled":    {fmt.Errorf("wrapped: %w", context.Canceled), "caller_cancelled"},
		"plain error":         {errors.New("json: cannot unmarshal string"), "error"},
		"network error":       {&url.Error{Op: "Get", URL: "x", Err: errors.New("eof")}, "NETWORK_ERROR"},
		"typed error":         {&json.SyntaxError{}, "json.SyntaxError"},
		"stream parse failed": {core.NewStreamingError("bad chunk", nil), "STREAMING_ERROR"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tc.reason, FailureOf(tc.err).Reason(false))
		})
	}
}

func TestApplyFailedCall_MetadataErrorReasonStillOverrides(t *testing.T) {
	payload := failedChatRecord(FailureOf(providerAnswer(500, `{}`)), map[string]interface{}{"errorReason": "nightly batch failed", "agent": "a-1"})

	assert.Equal(t, "nightly batch failed", payload.ErrorReason)
	assert.Equal(t, "a-1", payload.Agent)
	assert.Equal(t, 500, *payload.ErrorCode)
}

func TestApplyFailedCall_DoesNotTakeTheIdsTheRetryOfTheCallWillCarry(t *testing.T) {
	metadata := map[string]interface{}{"transactionId": "tx-order-42", "idempotencyKey": "order-42"}
	payload := failedChatRecord(FailureOf(providerAnswer(429, `{}`)), metadata)

	assert.NotEqual(t, "tx-order-42", payload.TransactionID)
	assert.NotEmpty(t, payload.TransactionID)
	assert.Regexp(t, `^order-42:err:[0-9a-f]{8}$`, payload.IdempotencyKey)

	long := strings.Repeat("k", maxIdempotencyKeyLength)
	payload = failedChatRecord(FailureOf(providerAnswer(429, `{}`)), map[string]interface{}{"idempotencyKey": long})
	assert.Regexp(t, `^[0-9a-f]{64}:err:[0-9a-f]{8}$`, payload.IdempotencyKey)
	assert.True(t, isSendableIdempotencyKey(payload.IdempotencyKey))
}

func TestApplyFailedCall_GivesMediaRecordsTheZeroQuantityTheirEndpointRequires(t *testing.T) {
	image := NewPayload(OperationImage, "gpt-image-1", "OPENAI").Build()
	ApplyFailedCall(image, FailureOf(providerAnswer(400, `{}`)), nil)
	require.NotNil(t, image.ActualImageCount)
	assert.Equal(t, 0, *image.ActualImageCount)

	video := NewPayload(OperationVideo, "gen4_turbo", "runway").WithVideoDuration(0, 10).Build()
	ApplyFailedCall(video, FailureOf(providerAnswer(400, `{}`)), nil)
	assert.Equal(t, 0.0, *video.DurationSeconds)
	assert.Equal(t, 10.0, *video.RequestedDurationSeconds)

	video = NewPayload(OperationVideo, "gen4_turbo", "runway").Build()
	ApplyFailedCall(video, FailureOf(providerAnswer(400, `{}`)), nil)
	require.NotNil(t, video.DurationSeconds)
	assert.Equal(t, 0.0, *video.DurationSeconds)
}

func TestFailedCallRecord_ErrorCodeIsSentUnderThePlatformName(t *testing.T) {
	wire := serializedRecord(t, failedChatRecord(FailureOf(providerAnswer(429, `{}`)), nil))
	assert.Equal(t, float64(429), wire["errorCode"])
	assert.NotContains(t, wire, "failureCode")

	success := serializedRecord(t, NewPayload(OperationChat, "m", "OPENAI").Build())
	assert.NotContains(t, success, "errorCode")
}

func TestSend_CutsAnErrorReasonLongerThanTheStoredColumn(t *testing.T) {
	mock := testutil.NewMockMeteringServer()
	defer mock.Close()
	client := newTestClient(t, mock.URL())
	defer client.Close()

	payload := NewPayload(OperationChat, "m", "OPENAI").WithError(strings.Repeat("x", 300)).Build()
	require.NoError(t, client.SendSync(payload))

	require.Len(t, mock.GetPayloads(), 1)
	assert.Len(t, mock.GetPayloads()[0]["errorReason"], MaxErrorReasonLength)
}

func TestToolEvent_FailureSendsTheErrorKindNotItsText(t *testing.T) {
	t.Setenv(core.EnvCapturePrompts, "")
	err := &url.Error{Op: "Get", URL: "https://maps.example.com/geo?key=AIzaFAKE&email=jane@example.com", Err: errors.New("timeout awaiting headers")}

	t.Setenv(core.EnvCaptureErrorMessages, "")
	assert.Equal(t, "NETWORK_ERROR", NewToolEvent("geo").WithFailure(err).Build().ErrorMessage)

	t.Setenv(core.EnvCaptureErrorMessages, "true")
	message := NewToolEvent("geo").WithFailure(err).Build().ErrorMessage
	assert.True(t, strings.HasPrefix(message, "NETWORK_ERROR: Get"), message)
	assert.NotContains(t, message, "AIzaFAKE")
	assert.NotContains(t, message, "jane@example.com")
	assert.False(t, NewToolEvent("geo").WithFailure(err).Build().Success)
}

func TestToolEvent_WithErrorRedactsAndCapsTheCallersText(t *testing.T) {
	message := NewToolEvent("crm").WithError("lookup for jane@example.com failed: Bearer abcdefghijklmnop " + strings.Repeat("y", 3000)).Build().ErrorMessage

	assert.NotContains(t, message, "jane@example.com")
	assert.NotContains(t, message, "abcdefghijklmnop")
	assert.Equal(t, MaxToolErrorMessageLength, utf8.RuneCountInString(message))
}

func TestApplyFailedCall_SameProviderOperationGetsTheSameIdempotencyKey(t *testing.T) {
	metadata := map[string]interface{}{"idempotencyKey": "veo-job-7"}
	reported := func(transactionID string) string {
		payload := NewPayload(OperationVideo, "veo-3", "GOOGLE").WithTransactionID(transactionID).Build()
		ApplyFailedCall(payload, FailureOf(providerAnswer(400, `{}`)), metadata)
		return payload.IdempotencyKey
	}

	assert.Equal(t, reported("op-tx-1"), reported("op-tx-1"), "a second report of one operation must be deduplicated")
	assert.NotEqual(t, reported("op-tx-1"), reported("op-tx-2"))
	assert.Regexp(t, `^veo-job-7:err:[0-9a-f]{8}$`, reported("op-tx-1"))
}

type bedrockCodedError struct{}

func (bedrockCodedError) Error() string {
	return "ValidationException: prompt 'say hello please' is too long"
}
func (bedrockCodedError) ErrorCode() string { return "ValidationException" }

func TestFailureOf_ErrorWithAProviderCodeIsTheProvidersTextEvenWithoutAStatus(t *testing.T) {
	t.Setenv(core.EnvCapturePrompts, "")
	t.Setenv(core.EnvCaptureErrorMessages, "true")
	failure := FailureOf(fmt.Errorf("bedrock api error: %w", bedrockCodedError{}))

	assert.True(t, failure.ProviderReply)
	assert.Equal(t, "ValidationException", failure.Reason(false), "without prompt capture the provider's text stays out")
	assert.Equal(t, "ValidationException: bedrock api error: ValidationException: prompt 'say hello please' is too long", failure.Reason(true))
}

func TestFailureOf_ProviderReplyWithoutAStatusNeedsPromptCapture(t *testing.T) {
	t.Setenv(core.EnvCapturePrompts, "")
	t.Setenv(core.EnvCaptureErrorMessages, "true")
	err := core.NewProviderError(`stream error: {"error":{"message":"bad 'say hello please'"}}`, nil).
		WithProviderReply([]byte(`{"error":{"message":"bad 'say hello please'"}}`))

	assert.Equal(t, "PROVIDER_ERROR", FailureOf(err).Reason(false))
}
