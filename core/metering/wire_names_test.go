package metering

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func serializedRecord(t *testing.T, payload interface{}) map[string]interface{} {
	t.Helper()
	data, err := json.Marshal(payload)
	require.NoError(t, err)
	var wire map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &wire))
	return wire
}

func TestMeteringRecord_SerializesEveryFieldUnderTheNameReveniumReads(t *testing.T) {
	retry := 2
	temperature := 0.4
	inputCost, outputCost, cacheWriteCost, cacheReadCost, totalCost := 0.1, 0.2, 0.3, 0.4, 1.0
	completionStart := time.Date(2026, 10, 1, 12, 0, 1, 0, time.UTC)

	payload := NewPayload(OperationChat, "gpt-4.1-nano", "OPENAI").
		WithTiming(time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC), 2*time.Second).
		WithTokens(100, 20, 120).
		WithReasoningTokens(5, 7, 9).
		WithStreaming(true, 250, &completionStart).
		WithStopReason("TOKEN_LIMIT").
		WithModelSource("OPENAI").
		WithSystemFingerprint("fp_1").
		WithTemperature(temperature).
		WithTransactionID("tx-1").
		WithCosts(&inputCost, &outputCost, &cacheWriteCost, &cacheReadCost, &totalCost).
		Build()
	payload.ErrorReason = "upstream said no"
	payload.OrganizationName = "org-1"
	payload.ProductName = "product-1"
	payload.TaskType = "summarize"
	payload.Agent = "agent-1"
	payload.SubscriptionID = "subscription-1"
	payload.TraceID = "trace-1"
	payload.ParentTransactionID = "parent-tx-1"
	payload.TraceType = "workflow"
	payload.TraceName = "nightly"
	payload.TransactionName = "step-1"
	payload.TicketID = "ticket-1"
	payload.AgenticJobID = "job-1"
	payload.Environment = "staging"
	payload.Region = "us-east-1"
	payload.RetryNumber = &retry
	payload.Subscriber = map[string]interface{}{"id": "sub-1", "email": "sub@example.com"}

	wire := serializedRecord(t, payload)

	expected := map[string]interface{}{
		"provider":                "OPENAI",
		"model":                   "gpt-4.1-nano",
		"modelSource":             "OPENAI",
		"operationType":           "CHAT",
		"costType":                "AI",
		"middlewareSource":        "revenium-go-sdk",
		"transactionId":           "tx-1",
		"stopReason":              "TOKEN_LIMIT",
		"errorReason":             "upstream said no",
		"isStreamed":              true,
		"inputTokenCount":         float64(100),
		"outputTokenCount":        float64(20),
		"totalTokenCount":         float64(120),
		"reasoningTokenCount":     float64(5),
		"cacheCreationTokenCount": float64(7),
		"cacheReadTokenCount":     float64(9),
		"requestTime":             "2026-10-01T12:00:00Z",
		"responseTime":            "2026-10-01T12:00:02Z",
		"completionStartTime":     "2026-10-01T12:00:01Z",
		"requestDuration":         float64(2000),
		"timeToFirstToken":        float64(250),
		"systemFingerprint":       "fp_1",
		"temperature":             0.4,
		"inputTokenCost":          0.1,
		"outputTokenCost":         0.2,
		"cacheCreationTokenCost":  0.3,
		"cacheReadTokenCost":      0.4,
		"totalCost":               1.0,
		"organizationName":        "org-1",
		"productName":             "product-1",
		"taskType":                "summarize",
		"agent":                   "agent-1",
		"subscriptionId":          "subscription-1",
		"traceId":                 "trace-1",
		"parentTransactionId":     "parent-tx-1",
		"traceType":               "workflow",
		"traceName":               "nightly",
		"transactionName":         "step-1",
		"ticketId":                "ticket-1",
		"agenticJobId":            "job-1",
		"environment":             "staging",
		"region":                  "us-east-1",
		"retryNumber":             float64(2),
		"subscriber":              map[string]interface{}{"id": "sub-1", "email": "sub@example.com"},
	}
	for name, want := range expected {
		assert.Equal(t, want, wire[name], "serialized field %q", name)
	}
}
