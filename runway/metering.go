package runway

import (
	"errors"
	"time"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/revenium/revenium-go-sdk/core/metering"
	"github.com/revenium/revenium-go-sdk/core/prompt"
)

// buildVideoMeteringPayload records a task that succeeded, or one the SDK
// stopped waiting for, whose result.Error is the SDK's own reason. A task
// Runway reports failed or canceled is recorded by buildEndedTaskPayload.
func buildVideoMeteringPayload(result *VideoGenerationResult, subtype string, videoSeconds float64, metadata map[string]interface{}, capturePrompts bool, startTime time.Time) *metering.MeteringPayload {
	b := metering.NewPayload(metering.OperationVideo, result.Model, "runway").
		WithTiming(startTime, result.Duration).
		WithModelSource("RUNWAY").
		WithOperationSubtype(subtype).
		WithTransactionID(result.ID)

	switch {
	case result.Error != nil:
		b.WithError(*result.Error)
	case !isBillableSeconds(videoSeconds):
		b.WithError(unknownVideoLengthReason(result.Model))
	}

	b.WithVideoDuration(videoSeconds, videoSeconds)

	payload := b.Build()

	metering.ApplyMetadata(payload, metadata)

	if capturePrompts {
		metering.ApplyCapturedPrompt(payload, capturedPrompt(result), capturedOutput(result))
	}

	return payload
}

func taskRecordStart(record taskRecord, startTime time.Time) *metering.PayloadBuilder {
	return metering.NewPayload(metering.OperationVideo, record.model, "runway").
		WithTiming(startTime, time.Since(startTime)).
		WithModelSource("RUNWAY").
		WithOperationSubtype(record.subtype).
		WithVideoDuration(0, record.videoSeconds)
}

// buildRejectedTaskPayload records a task Runway refused to create.
func buildRejectedTaskPayload(record taskRecord, startTime time.Time, err error, capturePrompts bool) *metering.MeteringPayload {
	payload := taskRecordStart(record, startTime).Build()
	metering.ApplyFailedCall(payload, metering.FailureOf(err), prompt.WithClientCaptureSetting(record.metadata, capturePrompts))
	return payload
}

// buildEndedTaskPayload records a task Runway reports failed or canceled.
// Runway bills neither, so the record has no length.
func buildEndedTaskPayload(task acceptedTask, status *TaskStatusResponse, capturePrompts bool) *metering.MeteringPayload {
	payload := taskRecordStart(task.taskRecord, task.startTime).WithTransactionID(task.id).Build()
	metadata := prompt.WithClientCaptureSetting(task.metadata, capturePrompts)
	if status.Status == TaskStatusCanceled {
		metering.ApplyCancelledCall(payload, metadata)
		return payload
	}
	metering.ApplyFailedCall(payload, taskFailure(status), metadata)
	return payload
}

// taskFailure names a failed task by Runway's failure code, such as
// "SAFETY.INPUT.TEXT"; its message can quote the prompt.
func taskFailure(status *TaskStatusResponse) metering.CallFailure {
	message := "task failed"
	if status.Error != nil {
		message = *status.Error
	}
	var kind string
	if status.FailureCode != nil {
		kind = core.ErrorKindFrom(*status.FailureCode)
	}
	if kind == "" {
		kind = "task_failed"
	}
	return metering.CallFailure{Err: errors.New(message), Kind: kind, ProviderReply: true}
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
