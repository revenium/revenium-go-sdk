package runway

import (
	"encoding/json"
	"time"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/revenium/revenium-go-sdk/core/metering"
)

func mapRunwayStopReason(status TaskStatus, err *string) string {
	if err != nil {
		return "ERROR"
	}
	switch status {
	case TaskStatusFailed:
		return "ERROR"
	case TaskStatusCanceled:
		return "CANCELLED"
	default:
		return "END"
	}
}

func buildVideoMeteringPayload(result *VideoGenerationResult, metadata *UsageMetadata, capturePrompts bool, startTime time.Time) *metering.MeteringPayload {
	b := metering.NewPayload(metering.OperationVideo, result.Model, "runway").
		WithTiming(startTime, result.Duration).
		WithModelSource("RUNWAY").
		WithTransactionID(result.ID).
		WithStopReason(mapRunwayStopReason(result.Status, result.Error))

	if result.Error != nil {
		b.WithError(*result.Error)
	}
	if result.FailureCode != nil {
		b.WithFailureCode(*result.FailureCode)
	}

	var videoDuration float64 = 5.0
	var requestedDuration float64 = 5.0

	if result.Metadata != nil {
		if dur, ok := result.Metadata["duration"].(int); ok {
			videoDuration = float64(dur)
		} else if dur, ok := result.Metadata["duration"].(float64); ok {
			videoDuration = dur
		} else if dur, ok := result.Metadata["durationSeconds"].(float64); ok {
			videoDuration = dur
		}

		if reqDur, ok := result.Metadata["requestedDuration"].(int); ok {
			requestedDuration = float64(reqDur)
		} else if reqDur, ok := result.Metadata["requestedDuration"].(float64); ok {
			requestedDuration = reqDur
		} else if reqDur, ok := result.Metadata["requestedDurationSeconds"].(float64); ok {
			requestedDuration = reqDur
		} else {
			requestedDuration = videoDuration
		}
	}

	b.WithVideoDuration(videoDuration, requestedDuration)

	payload := b.Build()

	if metadata != nil {
		md := usageMetadataToMap(metadata)
		metering.ApplyMetadata(payload, md)
	}

	if capturePrompts && result.Metadata != nil {
		if prompt, ok := result.Metadata["_capturedPrompt"].(string); ok && prompt != "" {
			inputMessages, truncated := metering.FormatPromptAsInputMessages(prompt)
			if inputMessages != "" {
				payload.InputMessages = inputMessages
			}
			payload.PromptsTruncated = truncated
			core.Debug("Prompt capture enabled: captured %d chars", len(prompt))
		}
		if len(result.OutputURLs) > 0 {
			outputJSON, err := json.Marshal(result.OutputURLs)
			if err == nil {
				payload.OutputResponse = string(outputJSON)
			}
		}
	}

	return payload
}

func usageMetadataToMap(metadata *UsageMetadata) map[string]interface{} {
	if metadata == nil {
		return nil
	}

	m := make(map[string]interface{})

	if metadata.OrganizationName != "" {
		m["organizationName"] = metadata.OrganizationName
	}
	if metadata.ProductName != "" {
		m["productName"] = metadata.ProductName
	}
	if metadata.TaskType != "" {
		m["taskType"] = metadata.TaskType
	}
	if metadata.Agent != "" {
		m["agent"] = metadata.Agent
	}
	if metadata.SubscriptionID != "" {
		m["subscriptionId"] = metadata.SubscriptionID
	}
	if metadata.TraceID != "" {
		m["traceId"] = metadata.TraceID
	}
	if metadata.ParentTransactionID != "" {
		m["parentTransactionId"] = metadata.ParentTransactionID
	}
	if metadata.TraceType != "" {
		m["traceType"] = metadata.TraceType
	}
	if metadata.TraceName != "" {
		m["traceName"] = metadata.TraceName
	}
	if metadata.TicketID != "" {
		m["ticketId"] = metadata.TicketID
	}
	if metadata.Environment != "" {
		m["environment"] = metadata.Environment
	}
	if metadata.Region != "" {
		m["region"] = metadata.Region
	}
	if metadata.RetryNumber != nil {
		m["retryNumber"] = float64(*metadata.RetryNumber)
	}
	if metadata.CredentialAlias != "" {
		m["credentialAlias"] = metadata.CredentialAlias
	}
	if metadata.Subscriber != nil {
		m["subscriber"] = metadata.Subscriber
	}
	if metadata.TaskID != "" {
		m["taskId"] = metadata.TaskID
	}
	if metadata.ResponseQualityScore != nil {
		m["responseQualityScore"] = *metadata.ResponseQualityScore
	}
	if metadata.VideoJobID != "" {
		m["videoJobId"] = metadata.VideoJobID
	}
	if metadata.AudioJobID != "" {
		m["audioJobId"] = metadata.AudioJobID
	}
	if metadata.Custom != nil {
		for k, v := range metadata.Custom {
			if _, exists := m[k]; !exists {
				m[k] = v
			}
		}
	}

	return m
}
