package metering

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewToolEvent_Defaults(t *testing.T) {
	p := NewToolEvent("web_scraper").Build()

	assert.Equal(t, "web_scraper", p.ToolID)
	assert.Equal(t, "execute", p.Operation)
	assert.True(t, p.Success)
	assert.NotEmpty(t, p.TransactionID)
	assert.NotEmpty(t, p.IdempotencyKey)
	assert.NotEqual(t, p.TransactionID, p.IdempotencyKey)
	assert.NotEmpty(t, p.Timestamp)
	assert.Equal(t, int64(0), p.DurationMs)
	assert.Empty(t, p.ErrorMessage)
	assert.Nil(t, p.CostUsd)
	assert.Empty(t, p.Agent)
	assert.Empty(t, p.OrganizationName)
	assert.Empty(t, p.ProductName)
}

func TestToolEventBuilder_Chain(t *testing.T) {
	ts := time.Date(2025, 6, 15, 10, 30, 0, 0, time.UTC)

	p := NewToolEvent("firecrawl").
		WithOperation("scrape").
		WithDuration(2500 * time.Millisecond).
		WithSuccess(true).
		WithCost(0.005).
		WithAgent("research-assistant").
		WithOrganization("org-123").
		WithProduct("customer-portal").
		WithSubscriberCredential("sub-456").
		WithWorkflowID("wf-789").
		WithTraceID("trace-abc").
		WithTransactionID("txn-custom").
		WithTimestamp(ts).
		WithUsageMetadata(map[string]interface{}{"pages": 5}).
		Build()

	assert.Equal(t, "firecrawl", p.ToolID)
	assert.Equal(t, "scrape", p.Operation)
	assert.Equal(t, int64(2500), p.DurationMs)
	assert.True(t, p.Success)
	assert.Equal(t, 0.005, *p.CostUsd)
	assert.Equal(t, "research-assistant", p.Agent)
	assert.Equal(t, "org-123", p.OrganizationName)
	assert.Equal(t, "customer-portal", p.ProductName)
	assert.Equal(t, "sub-456", p.SubscriberCredential)
	assert.Equal(t, "wf-789", p.WorkflowID)
	assert.Equal(t, "trace-abc", p.TraceID)
	assert.Equal(t, "txn-custom", p.TransactionID)
	assert.Equal(t, "2025-06-15T10:30:00Z", p.Timestamp)
	assert.Equal(t, 5, p.UsageMetadata["pages"])
}

func TestToolEventBuilder_WithError(t *testing.T) {
	p := NewToolEvent("github_api").
		WithError("repository not found").
		Build()

	assert.False(t, p.Success)
	assert.Equal(t, "repository not found", p.ErrorMessage)
}

func TestToolEventBuilder_WithSuccessClearsErrorMessage(t *testing.T) {
	p := NewToolEvent("tool").
		WithError("some error").
		WithSuccess(true).
		Build()

	assert.True(t, p.Success)
	assert.Empty(t, p.ErrorMessage)
}

func TestToolEventBuilder_EmptyStringsIgnored(t *testing.T) {
	p := NewToolEvent("tool").
		WithOperation("").
		WithAgent("").
		WithOrganization("").
		WithProduct("").
		WithTransactionID("").
		Build()

	assert.Equal(t, "execute", p.Operation)
	assert.Empty(t, p.Agent)
	assert.NotEmpty(t, p.TransactionID)
}

func TestToolEventPayload_JSONWireFormat(t *testing.T) {
	ts := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)

	p := NewToolEvent("web_scraper").
		WithOperation("fetch").
		WithDuration(1234 * time.Millisecond).
		WithTimestamp(ts).
		WithTransactionID("txn-001").
		WithIdempotencyKey("idem-key-001").
		Build()

	data, err := json.Marshal(p)
	require.NoError(t, err)

	var m map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &m))

	assert.Equal(t, "txn-001", m["transactionId"])
	assert.Equal(t, "web_scraper", m["toolId"])
	assert.Equal(t, "fetch", m["operation"])
	assert.Equal(t, float64(1234), m["durationMs"])
	assert.Equal(t, true, m["success"])
	assert.Equal(t, "2025-01-15T12:00:00Z", m["timestamp"])

	_, hasError := m["errorMessage"]
	assert.False(t, hasError, "empty errorMessage should be omitted")

	_, hasCost := m["costUsd"]
	assert.False(t, hasCost, "nil costUsd should be omitted")

	_, hasAgent := m["agent"]
	assert.False(t, hasAgent, "empty agent should be omitted")

	_, hasIdempotencyKey := m["idempotencyKey"]
	assert.False(t, hasIdempotencyKey, "idempotencyKey must not appear in JSON body")

	assert.Equal(t, "idem-key-001", p.IdempotencyKey)
}

func TestApplyToolEventMetadata_StringFields(t *testing.T) {
	p := NewToolEvent("tool").Build()
	ApplyToolEventMetadata(p, map[string]interface{}{
		"agent":                "my-agent",
		"organizationName":    "org-123",
		"productName":         "prod-456",
		"subscriberCredential": "sub-789",
		"workflowId":          "wf-1",
		"traceId":             "trace-1",
		"transactionId":       "custom-tx",
	})

	assert.Equal(t, "my-agent", p.Agent)
	assert.Equal(t, "org-123", p.OrganizationName)
	assert.Equal(t, "prod-456", p.ProductName)
	assert.Equal(t, "sub-789", p.SubscriberCredential)
	assert.Equal(t, "wf-1", p.WorkflowID)
	assert.Equal(t, "trace-1", p.TraceID)
	assert.Equal(t, "custom-tx", p.TransactionID)
}

func TestApplyToolEventMetadata_AliasFields(t *testing.T) {
	p := NewToolEvent("tool").Build()
	ApplyToolEventMetadata(p, map[string]interface{}{
		"organizationId": "org-alias",
		"productId":      "prod-alias",
		"subscriptionId": "sub-alias",
	})

	assert.Equal(t, "org-alias", p.OrganizationName)
	assert.Equal(t, "prod-alias", p.ProductName)
	assert.Equal(t, "sub-alias", p.SubscriberCredential)
}

func TestApplyToolEventMetadata_CanonicalPrecedenceOverAlias(t *testing.T) {
	p := NewToolEvent("tool").Build()
	ApplyToolEventMetadata(p, map[string]interface{}{
		"organizationName": "canonical-org",
		"organizationId":   "alias-org",
		"productName":      "canonical-prod",
		"productId":        "alias-prod",
	})

	assert.Equal(t, "canonical-org", p.OrganizationName)
	assert.Equal(t, "canonical-prod", p.ProductName)
}

func TestApplyToolEventMetadata_AliasFallbackWhenCanonicalAbsent(t *testing.T) {
	p := NewToolEvent("tool").Build()
	ApplyToolEventMetadata(p, map[string]interface{}{
		"organizationId": "alias-org",
		"productId":      "alias-prod",
		"subscriptionId": "alias-sub",
	})

	assert.Equal(t, "alias-org", p.OrganizationName)
	assert.Equal(t, "alias-prod", p.ProductName)
	assert.Equal(t, "alias-sub", p.SubscriberCredential)
}

func TestApplyToolEventMetadata_UsageMetadata(t *testing.T) {
	p := NewToolEvent("tool").Build()
	meta := map[string]interface{}{"pages": 5, "data_mb": 2.3}
	ApplyToolEventMetadata(p, map[string]interface{}{
		"usageMetadata": meta,
	})

	assert.Equal(t, meta, p.UsageMetadata)
}

func TestApplyToolEventMetadata_Nil(t *testing.T) {
	p := NewToolEvent("tool").Build()
	ApplyToolEventMetadata(p, nil)
	assert.True(t, p.Success)
}

func TestToolEventBuilder_IdempotencyKeyOverride(t *testing.T) {
	p := NewToolEvent("tool").
		WithIdempotencyKey("my-custom-key").
		Build()
	assert.Equal(t, "my-custom-key", p.IdempotencyKey)
}

func TestToolEventBuilder_EmptyIdempotencyKeyIgnored(t *testing.T) {
	p := NewToolEvent("tool").
		WithIdempotencyKey("").
		Build()
	assert.NotEmpty(t, p.IdempotencyKey)
}

func TestApplyToolEventMetadata_IdempotencyKey(t *testing.T) {
	p := NewToolEvent("tool").Build()
	ApplyToolEventMetadata(p, map[string]interface{}{
		"idempotencyKey": "meta-key-789",
	})
	assert.Equal(t, "meta-key-789", p.IdempotencyKey)
}

func TestToolEventEndpoint(t *testing.T) {
	tests := []struct {
		name     string
		baseURL  string
		expected string
	}{
		{"default", "", "https://api.revenium.ai/meter/v2/tool/events"},
		{"custom base", "https://custom.api.com", "https://custom.api.com/meter/v2/tool/events"},
		{"trailing slash", "https://custom.api.com/", "https://custom.api.com/meter/v2/tool/events"},
		{"legacy meter/v2", "https://custom.api.com/meter/v2", "https://custom.api.com/meter/v2/tool/events"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, ToolEventEndpoint(tt.baseURL))
		})
	}
}
