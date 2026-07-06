package metering

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
	{"transactionId", func(p *ToolEventPayload) *string { return &p.TransactionID }},
	{"idempotencyKey", func(p *ToolEventPayload) *string { return &p.IdempotencyKey }},
}

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
}

func applyToolEventStringField(payload *ToolEventPayload, metadata map[string]interface{}, f toolEventFieldMapping) {
	val, ok := metadata[f.key]
	if !ok {
		return
	}
	if s, ok := val.(string); ok && s != "" {
		*f.accessor(payload) = s
	}
}
