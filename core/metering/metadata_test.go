package metering

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestApplyMetadata_StringFields(t *testing.T) {
	p := NewPayload(OperationChat, "gpt-4", "OPENAI").Build()
	ApplyMetadata(p, map[string]interface{}{
		"organizationId":      "org-123",
		"productId":           "prod-456",
		"taskType":            "summarize",
		"agent":               "my-agent",
		"subscriptionId":      "sub-789",
		"traceId":             "trace-1",
		"parentTransactionId": "parent-1",
		"traceType":           "chain",
		"traceName":           "my-chain",
		"environment":         "production",
		"region":              "us-east-1",
		"credentialAlias":     "default",
		"taskId":              "task-1",
		"modelSource":         "CUSTOM",
		"transactionId":       "custom-tx",
	})

	assert.Equal(t, "org-123", p.OrganizationName)
	assert.Equal(t, "prod-456", p.ProductName)
	assert.Equal(t, "summarize", p.TaskType)
	assert.Equal(t, "my-agent", p.Agent)
	assert.Equal(t, "sub-789", p.SubscriptionID)
	assert.Equal(t, "trace-1", p.TraceID)
	assert.Equal(t, "parent-1", p.ParentTransactionID)
	assert.Equal(t, "chain", p.TraceType)
	assert.Equal(t, "my-chain", p.TraceName)
	assert.Equal(t, "production", p.Environment)
	assert.Equal(t, "us-east-1", p.Region)
	assert.Equal(t, "default", p.CredentialAlias)
	assert.Equal(t, "task-1", p.TaskID)
	assert.Equal(t, "CUSTOM", p.ModelSource)
	assert.Equal(t, "custom-tx", p.TransactionID)
}

func TestApplyMetadata_FloatFields(t *testing.T) {
	p := NewPayload(OperationChat, "gpt-4", "OPENAI").Build()
	ApplyMetadata(p, map[string]interface{}{
		"temperature":          0.7,
		"responseQualityScore": 0.95,
		"inputTokenCost":       0.001,
		"outputTokenCost":      0.002,
		"totalCost":            0.5,
	})

	assert.Equal(t, 0.7, *p.Temperature)
	assert.Equal(t, 0.95, *p.ResponseQualityScore)
	assert.Equal(t, 0.001, *p.InputTokenCost)
	assert.Equal(t, 0.002, *p.OutputTokenCost)
	assert.Equal(t, 0.5, *p.TotalCost)
}

func TestApplyMetadata_IntToFloat(t *testing.T) {
	p := NewPayload(OperationChat, "gpt-4", "OPENAI").Build()
	ApplyMetadata(p, map[string]interface{}{
		"totalCost": 5,
	})
	assert.Equal(t, 5.0, *p.TotalCost)
}

func TestApplyMetadata_ErrorReasonOverridesStopReason(t *testing.T) {
	p := NewPayload(OperationChat, "gpt-4", "OPENAI").Build()
	assert.Equal(t, "END", p.StopReason)

	ApplyMetadata(p, map[string]interface{}{
		"errorReason": "rate_limit_exceeded",
	})

	assert.Equal(t, "ERROR", p.StopReason)
	assert.Equal(t, "rate_limit_exceeded", p.ErrorReason)
}

func TestApplyMetadata_RetryNumber(t *testing.T) {
	p := NewPayload(OperationChat, "gpt-4", "OPENAI").Build()
	ApplyMetadata(p, map[string]interface{}{
		"retryNumber": 3,
	})
	assert.Equal(t, 3, *p.RetryNumber)
}

func TestApplyMetadata_Subscriber(t *testing.T) {
	p := NewPayload(OperationChat, "gpt-4", "OPENAI").Build()
	sub := map[string]interface{}{"id": "user-1", "plan": "pro"}
	ApplyMetadata(p, map[string]interface{}{
		"subscriber": sub,
	})
	assert.Equal(t, sub, p.Subscriber)
}

func TestApplyMetadata_CanonicalOverridesDeprecated(t *testing.T) {
	p := NewPayload(OperationChat, "gpt-4", "OPENAI").Build()
	ApplyMetadata(p, map[string]interface{}{
		"organizationId":   "old-org",
		"organizationName": "new-org",
		"productId":        "old-prod",
		"productName":      "new-prod",
	})

	assert.Equal(t, "new-org", p.OrganizationName)
	assert.Equal(t, "new-prod", p.ProductName)
}

func TestApplyMetadata_Nil(t *testing.T) {
	p := NewPayload(OperationChat, "gpt-4", "OPENAI").Build()
	ApplyMetadata(p, nil)
	assert.Equal(t, "END", p.StopReason)
}
