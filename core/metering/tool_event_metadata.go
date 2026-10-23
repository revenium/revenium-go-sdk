package metering

import "github.com/revenium/revenium-go-sdk/core"

type toolEventFieldMapping struct {
	key      string
	accessor func(*ToolEventPayload) *string
}

var canonicalToolEventFields = []toolEventFieldMapping{
	{"agent", func(p *ToolEventPayload) *string { return &p.Agent }},
	{"organizationName", func(p *ToolEventPayload) *string { return &p.OrganizationName }},
	{"productName", func(p *ToolEventPayload) *string { return &p.ProductName }},
	{"subscriberCredential", func(p *ToolEventPayload) *string { return &p.SubscriberCredential }},
	{"workflowId", func(p *ToolEventPayload) *string { return &p.WorkflowID }},
	{"traceId", func(p *ToolEventPayload) *string { return &p.TraceID }},
	{"agenticJobId", func(p *ToolEventPayload) *string { return &p.AgenticJobID }},
}

const callerTransactionIDKey = "callerTransactionId"

var aliasToolEventFields = []toolEventFieldMapping{
	{"organizationId", func(p *ToolEventPayload) *string { return &p.OrganizationName }},
	{"productId", func(p *ToolEventPayload) *string { return &p.ProductName }},
	{"subscriptionId", func(p *ToolEventPayload) *string { return &p.SubscriberCredential }},
}

func ApplyToolEventMetadata(payload *ToolEventPayload, metadata map[string]interface{}) {
	if payload == nil || metadata == nil {
		return
	}

	for _, f := range canonicalToolEventFields {
		applyToolEventStringField(payload, metadata, f)
	}

	for _, f := range aliasToolEventFields {
		target := f.accessor(payload)
		if *target != "" {
			continue
		}
		applyToolEventStringField(payload, metadata, f)
	}

	if v, ok := metadata["usageMetadata"].(map[string]interface{}); ok {
		payload.UsageMetadata = v
	}

	if callerTransactionID, ok := metadata["transactionId"].(string); ok && !core.IsBlank(callerTransactionID) {
		keepCallerTransactionID(payload, callerTransactionID)
	}
}

// keepCallerTransactionID records a transactionId the caller shares across
// several tool calls without making it the event's own id: Revenium refuses a
// second event with the same id or Idempotency-Key and rates only the first,
// so each event keeps its generated id (and the key that equals it) and the
// caller's value groups them as the trace id, or as
// usageMetadata.callerTransactionId when a trace id is already set. For the
// same reason an idempotencyKey in shared metadata is not applied to tool
// events; ToolEventBuilder.WithIdempotencyKey sets one for a single event.
func keepCallerTransactionID(payload *ToolEventPayload, callerTransactionID string) {
	if core.IsBlank(payload.TraceID) {
		payload.TraceID = callerTransactionID
		return
	}
	if _, set := payload.UsageMetadata[callerTransactionIDKey]; set {
		return
	}
	usageMetadata := make(map[string]interface{}, len(payload.UsageMetadata)+1)
	for key, value := range payload.UsageMetadata {
		usageMetadata[key] = value
	}
	usageMetadata[callerTransactionIDKey] = callerTransactionID
	payload.UsageMetadata = usageMetadata
}

func applyToolEventStringField(payload *ToolEventPayload, metadata map[string]interface{}, f toolEventFieldMapping) {
	val, ok := metadata[f.key]
	if !ok {
		return
	}
	if s, ok := val.(string); ok && !core.IsBlank(s) {
		if s = guardMetadataString(f.key, s); s == "" {
			return
		}
		*f.accessor(payload) = s
	}
}
