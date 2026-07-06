package metering

import "time"

type ToolEventPayload struct {
	IdempotencyKey       string                 `json:"-"`
	TransactionID        string                 `json:"transactionId,omitempty"`
	ToolID               string                 `json:"toolId"`
	Operation            string                 `json:"operation,omitempty"`
	DurationMs           int64                  `json:"durationMs"`
	Success              bool                   `json:"success"`
	ErrorMessage         string                 `json:"errorMessage,omitempty"`
	CostUsd              *float64               `json:"costUsd,omitempty"`
	Timestamp            string                 `json:"timestamp"`
	Agent                string                 `json:"agent,omitempty"`
	OrganizationName     string                 `json:"organizationName,omitempty"`
	ProductName          string                 `json:"productName,omitempty"`
	SubscriberCredential string                 `json:"subscriberCredential,omitempty"`
	WorkflowID           string                 `json:"workflowId,omitempty"`
	TraceID              string                 `json:"traceId,omitempty"`
	UsageMetadata        map[string]interface{} `json:"usageMetadata,omitempty"`
}

type ToolEventBuilder struct {
	payload *ToolEventPayload
}

func NewToolEvent(toolID string) *ToolEventBuilder {
	return &ToolEventBuilder{
		payload: &ToolEventPayload{
			IdempotencyKey: GenerateTransactionID(),
			TransactionID:  GenerateTransactionID(),
			ToolID:         toolID,
			Operation:      "execute",
			Success:        true,
			Timestamp:      time.Now().UTC().Format(time.RFC3339),
		},
	}
}

func (b *ToolEventBuilder) WithOperation(op string) *ToolEventBuilder {
	if op != "" {
		b.payload.Operation = op
	}
	return b
}

func (b *ToolEventBuilder) WithDuration(d time.Duration) *ToolEventBuilder {
	b.payload.DurationMs = d.Milliseconds()
	return b
}

func (b *ToolEventBuilder) WithSuccess(success bool) *ToolEventBuilder {
	b.payload.Success = success
	if success {
		b.payload.ErrorMessage = ""
	}
	return b
}

func (b *ToolEventBuilder) WithError(msg string) *ToolEventBuilder {
	b.payload.Success = false
	b.payload.ErrorMessage = msg
	return b
}

func (b *ToolEventBuilder) WithCost(usd float64) *ToolEventBuilder {
	b.payload.CostUsd = &usd
	return b
}

func (b *ToolEventBuilder) WithAgent(agent string) *ToolEventBuilder {
	if agent != "" {
		b.payload.Agent = agent
	}
	return b
}

func (b *ToolEventBuilder) WithOrganization(org string) *ToolEventBuilder {
	if org != "" {
		b.payload.OrganizationName = org
	}
	return b
}

func (b *ToolEventBuilder) WithProduct(product string) *ToolEventBuilder {
	if product != "" {
		b.payload.ProductName = product
	}
	return b
}

func (b *ToolEventBuilder) WithSubscriberCredential(cred string) *ToolEventBuilder {
	if cred != "" {
		b.payload.SubscriberCredential = cred
	}
	return b
}

func (b *ToolEventBuilder) WithWorkflowID(wf string) *ToolEventBuilder {
	if wf != "" {
		b.payload.WorkflowID = wf
	}
	return b
}

func (b *ToolEventBuilder) WithTraceID(trace string) *ToolEventBuilder {
	if trace != "" {
		b.payload.TraceID = trace
	}
	return b
}

func (b *ToolEventBuilder) WithUsageMetadata(meta map[string]interface{}) *ToolEventBuilder {
	b.payload.UsageMetadata = meta
	return b
}

func (b *ToolEventBuilder) WithTransactionID(id string) *ToolEventBuilder {
	if id != "" {
		b.payload.TransactionID = id
	}
	return b
}

func (b *ToolEventBuilder) WithIdempotencyKey(key string) *ToolEventBuilder {
	if key != "" {
		b.payload.IdempotencyKey = key
	}
	return b
}

func (b *ToolEventBuilder) WithTimestamp(t time.Time) *ToolEventBuilder {
	b.payload.Timestamp = t.UTC().Format(time.RFC3339)
	return b
}

func (b *ToolEventBuilder) Build() *ToolEventPayload {
	return b.payload
}
