package metering

import (
	"encoding/json"
	"strconv"

	"github.com/revenium/revenium-go-sdk/core"
)

var stringFields = map[string]*func(*MeteringPayload) *string{
	"organizationId":      strSetter(func(p *MeteringPayload) *string { return &p.OrganizationName }),
	"productId":           strSetter(func(p *MeteringPayload) *string { return &p.ProductName }),
	"organizationName":    strSetter(func(p *MeteringPayload) *string { return &p.OrganizationName }),
	"productName":         strSetter(func(p *MeteringPayload) *string { return &p.ProductName }),
	"taskType":            strSetter(func(p *MeteringPayload) *string { return &p.TaskType }),
	"agent":               strSetter(func(p *MeteringPayload) *string { return &p.Agent }),
	"subscriptionId":      strSetter(func(p *MeteringPayload) *string { return &p.SubscriptionID }),
	"traceId":             strSetter(func(p *MeteringPayload) *string { return &p.TraceID }),
	"parentTransactionId": strSetter(func(p *MeteringPayload) *string { return &p.ParentTransactionID }),
	"traceType":           strSetter(func(p *MeteringPayload) *string { return &p.TraceType }),
	"traceName":           strSetter(func(p *MeteringPayload) *string { return &p.TraceName }),
	"transactionName":     strSetter(func(p *MeteringPayload) *string { return &p.TransactionName }),
	"ticketId":            strSetter(func(p *MeteringPayload) *string { return &p.TicketID }),
	"agenticJobId":        strSetter(func(p *MeteringPayload) *string { return &p.AgenticJobID }),
	"agenticJobName":      strSetter(func(p *MeteringPayload) *string { return &p.AgenticJobName }),
	"agenticJobType":      strSetter(func(p *MeteringPayload) *string { return &p.AgenticJobType }),
	"agenticJobVersion":   strSetter(func(p *MeteringPayload) *string { return &p.AgenticJobVersion }),
	"environment":         strSetter(func(p *MeteringPayload) *string { return &p.Environment }),
	"region":              strSetter(func(p *MeteringPayload) *string { return &p.Region }),
	"credentialAlias":     strSetter(func(p *MeteringPayload) *string { return &p.CredentialAlias }),
	"taskId":              strSetter(func(p *MeteringPayload) *string { return &p.TaskID }),
	"videoJobId":          strSetter(func(p *MeteringPayload) *string { return &p.VideoJobID }),
	"audioJobId":          strSetter(func(p *MeteringPayload) *string { return &p.AudioJobID }),
	"modelSource":         strSetter(func(p *MeteringPayload) *string { return &p.ModelSource }),
	"systemFingerprint":   strSetter(func(p *MeteringPayload) *string { return &p.SystemFingerprint }),
	"errorReason":         nil,
	"transactionId":       strSetter(func(p *MeteringPayload) *string { return &p.TransactionID }),
	"idempotencyKey":      strSetter(func(p *MeteringPayload) *string { return &p.IdempotencyKey }),
	"failureCode":         strSetter(func(p *MeteringPayload) *string { return &p.FailureCode }),
}

var floatFields = map[string]func(*MeteringPayload, float64){
	"temperature":            func(p *MeteringPayload, v float64) { p.Temperature = &v },
	"mediationLatency":       func(p *MeteringPayload, v float64) { p.MediationLatency = &v },
	"responseQualityScore":   func(p *MeteringPayload, v float64) { p.ResponseQualityScore = &v },
	"inputTokenCost":         func(p *MeteringPayload, v float64) { p.InputTokenCost = &v },
	"outputTokenCost":        func(p *MeteringPayload, v float64) { p.OutputTokenCost = &v },
	"cacheCreationTokenCost": func(p *MeteringPayload, v float64) { p.CacheCreationTokenCost = &v },
	"cacheReadTokenCost":     func(p *MeteringPayload, v float64) { p.CacheReadTokenCost = &v },
	"totalCost":              func(p *MeteringPayload, v float64) { p.TotalCost = &v },
}

const retryNumberKey = "retryNumber"

func strSetter(fn func(*MeteringPayload) *string) *func(*MeteringPayload) *string {
	return &fn
}

func ApplyMetadata(payload *MeteringPayload, metadata map[string]interface{}) {
	if metadata == nil {
		return
	}

	for key, setter := range stringFields {
		if key == "organizationName" || key == "productName" {
			continue
		}
		val, ok := metadata[key]
		if !ok {
			continue
		}
		if key == "errorReason" {
			if s, ok := val.(string); ok && !core.IsBlank(s) {
				payload.ErrorReason = s
				payload.StopReason = "ERROR"
			}
			continue
		}
		if setter == nil {
			continue
		}
		if s, ok := val.(string); ok && !core.IsBlank(s) {
			if s = guardMetadataString(key, s); s == "" {
				continue
			}
			*(*setter)(payload) = s
		}
	}

	if s, ok := metadata["organizationName"].(string); ok && !core.IsBlank(s) {
		payload.OrganizationName = s
	}
	if s, ok := metadata["productName"].(string); ok && !core.IsBlank(s) {
		payload.ProductName = s
	}

	for key, setter := range floatFields {
		val, ok := metadata[key]
		if !ok {
			continue
		}
		f, ok := metadataNumber(val)
		if !ok {
			continue
		}
		if !isFinite(f) {
			warnDroppedMetadataNumber(key, f)
			continue
		}
		setter(payload, f)
	}

	if retry, ok := retryNumberFromMetadata(metadata[retryNumberKey]); ok {
		payload.RetryNumber = &retry
	}

	if v, ok := metadata["subscriber"].(map[string]interface{}); ok {
		payload.Subscriber = v
	}

	if s, ok := metadata["operationSubtype"].(string); ok {
		applyOperationSubtype(payload, s)
	}

	applyRecordFields(payload, metadata)
	warnUnrecognizedKeys(metadata)
}

func metadataNumber(val interface{}) (float64, bool) {
	switch v := val.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case json.Number:
		f, err := v.Float64()
		return f, err == nil
	case string:
		f, err := strconv.ParseFloat(v, 64)
		return f, err == nil
	}
	return 0, false
}
