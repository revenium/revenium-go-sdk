package jobs

import (
	"encoding/json"
	"time"
)

type ExecutionStatus string

const (
	ExecutionStatusSuccess   ExecutionStatus = "SUCCESS"
	ExecutionStatusFailed    ExecutionStatus = "FAILED"
	ExecutionStatusCancelled ExecutionStatus = "CANCELLED"
)

type OutcomeType string

const (
	OutcomeConverted    OutcomeType = "CONVERTED"
	OutcomeEscalated    OutcomeType = "ESCALATED"
	OutcomeDeflected    OutcomeType = "DEFLECTED"
	OutcomeUnsuccessful OutcomeType = "UNSUCCESSFUL"
	OutcomeCustom       OutcomeType = "CUSTOM"
)

type OutcomeAlreadyReportedError struct {
	JobID          string
	ReportedAt     *string
	AmendmentCount int
	Message        string
}

func (e *OutcomeAlreadyReportedError) Error() string {
	return e.Message
}

type OutcomeNotReportedError struct {
	JobID   string
	Message string
}

func (e *OutcomeNotReportedError) Error() string {
	return e.Message
}

// OutcomeAmendConflictError means the outcome changed after the version the
// amendment expected, or another amendment landed at the same time.
// CurrentEntityVersion is the version Revenium holds when the answer names it;
// re-check the outcome and resend with that version, or read it with GetJob.
type OutcomeAmendConflictError struct {
	JobID                string
	Message              string
	CurrentEntityVersion *int64
}

func (e *OutcomeAmendConflictError) Error() string {
	return e.Message
}

// JobOutcomeAmendment changes a reported outcome. Reason explains the change
// itself; OutcomeReason is why the job failed or was cancelled (nil leaves the
// stored one, an empty string clears it). Set ExpectedEntityVersion to the
// EntityVersion you last read so a change made since then is refused with
// OutcomeAmendConflictError instead of overwritten.
type JobOutcomeAmendment struct {
	Reason                string          `json:"reason"`
	ExpectedEntityVersion *int64          `json:"expectedEntityVersion,omitempty"`
	ExecutionStatus       ExecutionStatus `json:"executionStatus,omitempty"`
	OutcomeType           OutcomeType     `json:"outcomeType,omitempty"`
	OutcomeValue          *float64        `json:"outcomeValue,omitempty"`
	OutcomeCurrency       string          `json:"outcomeCurrency,omitempty"`
	Metadata              string          `json:"metadata,omitempty"`
	OutcomeReason         *string         `json:"outcomeReason,omitempty"`
	Metrics               []OutcomeMetric `json:"metrics,omitempty"`
	ReportedBy            string          `json:"reportedBy,omitempty"`
}

type MetricProvenance string

const (
	MetricMeasured     MetricProvenance = "MEASURED"
	MetricSelfReported MetricProvenance = "SELF_REPORTED"
	MetricDerived      MetricProvenance = "DERIVED"
	MetricAttested     MetricProvenance = "ATTESTED"
)

// OutcomeMetric is one per-job metric value. Its Key must be declared on the
// job type with resolution PER_JOB, or Revenium refuses it. Fields left empty
// take Revenium's defaults: provenance SELF_REPORTED, the calling key as
// RecordedBy, source "api" and the time Revenium receives it. Reason is
// required when the value replaces an earlier one for the same key.
type OutcomeMetric struct {
	Key        string           `json:"key"`
	Value      float64          `json:"value"`
	Provenance MetricProvenance `json:"provenance,omitempty"`
	RecordedBy string           `json:"recordedBy,omitempty"`
	Source     string           `json:"source,omitempty"`
	Reason     string           `json:"reason,omitempty"`
	RecordedAt *time.Time       `json:"recordedAt,omitempty"`
}

// JobOutcomeAmendmentEntry is one row of a job's outcome history: the
// initial report, then one row per amendment. Revenium does not return
// per-job metrics here or on JobResource.
type JobOutcomeAmendmentEntry struct {
	AmendmentSequence int             `json:"sequence"`
	ExecutionStatus   ExecutionStatus `json:"executionStatus"`
	OutcomeType       *OutcomeType    `json:"outcomeType,omitempty"`
	OutcomeValue      *float64        `json:"outcomeValue,omitempty"`
	OutcomeCurrency   *string         `json:"outcomeCurrency,omitempty"`
	OutcomeMetadata   *string         `json:"outcomeMetadata,omitempty"`
	OutcomeReason     *string         `json:"outcomeReason,omitempty"`
	ReportedBy        *string         `json:"reportedBy,omitempty"`
	ReportedAt        string          `json:"reportedAt"`
	Reason            *string         `json:"reason,omitempty"`
}

// JobOutcome is a job's final result. OutcomeReason says why a job failed or
// was cancelled; keep it out of Metadata. Metrics records per-job metric
// values with the outcome; values that arrive later go through
// AppendOutcomeMetrics.
type JobOutcome struct {
	ExecutionStatus ExecutionStatus `json:"executionStatus"`
	OutcomeType     OutcomeType     `json:"outcomeType,omitempty"`
	OutcomeValue    *float64        `json:"outcomeValue,omitempty"`
	OutcomeCurrency string          `json:"outcomeCurrency,omitempty"`
	Metadata        string          `json:"metadata,omitempty"`
	OutcomeReason   string          `json:"outcomeReason,omitempty"`
	Metrics         []OutcomeMetric `json:"metrics,omitempty"`
	ReportedBy      string          `json:"reportedBy,omitempty"`
}

type JobResource struct {
	ID                    string   `json:"id"`
	Label                 string   `json:"label"`
	ResourceType          string   `json:"resourceType"`
	Created               *string  `json:"created,omitempty"`
	Updated               *string  `json:"updated,omitempty"`
	AgenticJobID          string   `json:"agenticJobId"`
	Name                  *string  `json:"name,omitempty"`
	Type                  *string  `json:"type,omitempty"`
	Version               *string  `json:"version,omitempty"`
	Source                string   `json:"source"`
	ExecutionStatus       *string  `json:"executionStatus,omitempty"`
	OutcomeType           *string  `json:"outcomeType,omitempty"`
	OutcomeValue          *float64 `json:"outcomeValue,omitempty"`
	OutcomeCurrency       *string  `json:"outcomeCurrency,omitempty"`
	OutcomeReportedAt     *string  `json:"outcomeReportedAt,omitempty"`
	OutcomeMetadata       *string  `json:"outcomeMetadata,omitempty"`
	OutcomeReason         *string  `json:"outcomeReason,omitempty"`
	HasOutcome            bool     `json:"hasOutcome"`
	OutcomeAmendmentCount *int     `json:"outcomeUpdateCount,omitempty"`
	EntityVersion         *int64   `json:"entityVersion,omitempty"`
	OutcomeUpdatedAt      *string  `json:"outcomeUpdatedAt,omitempty"`
	OutcomeUpdatedBy      *string  `json:"outcomeUpdatedBy,omitempty"`

	exactOutcomeValue json.Number
}

// UnmarshalJSON also keeps OutcomeValue's exact decimal text, which a
// float64 cannot always hold at the 10 places Revenium stores.
func (r *JobResource) UnmarshalJSON(data []byte) error {
	type plainJobResource JobResource
	var exact struct {
		OutcomeValue json.Number `json:"outcomeValue"`
	}
	if err := json.Unmarshal(data, (*plainJobResource)(r)); err != nil {
		return err
	}
	if err := json.Unmarshal(data, &exact); err != nil {
		return err
	}
	r.exactOutcomeValue = exact.OutcomeValue
	return nil
}

type JobROIResource struct {
	AgenticJobID     string   `json:"agenticJobId"`
	AgenticJobName   *string  `json:"agenticJobName,omitempty"`
	AgenticJobType   *string  `json:"agenticJobType,omitempty"`
	TotalCost        float64  `json:"totalCost"`
	OutcomeValue     *float64 `json:"outcomeValue,omitempty"`
	OutcomeCurrency  *string  `json:"outcomeCurrency,omitempty"`
	ROI              *float64 `json:"roi,omitempty"`
	ExecutionStatus  *string  `json:"executionStatus,omitempty"`
	OutcomeType      *string  `json:"outcomeType,omitempty"`
	HasOutcome       bool     `json:"hasOutcome"`
	TransactionCount int64    `json:"transactionCount"`
	InputTokens      int64    `json:"inputTokens"`
	OutputTokens     int64    `json:"outputTokens"`
	TotalTokens      int64    `json:"totalTokens"`
}

type JobTimelineEvent struct {
	TransactionID string   `json:"transactionId"`
	Timestamp     string   `json:"timestamp"`
	Agent         *string  `json:"agent,omitempty"`
	Model         *string  `json:"model,omitempty"`
	Provider      *string  `json:"provider,omitempty"`
	Duration      *int64   `json:"duration,omitempty"`
	Cost          *float64 `json:"cost,omitempty"`
	InputTokens   *int64   `json:"inputTokens,omitempty"`
	OutputTokens  *int64   `json:"outputTokens,omitempty"`
	TotalTokens   *int64   `json:"totalTokens,omitempty"`
	Status        *string  `json:"status,omitempty"`
}

type JobTimelineResource struct {
	Transactions []JobTimelineEvent `json:"transactions"`
	TotalCount   int                `json:"totalCount"`
}

type ConversionFunnelResource struct {
	TotalJobs      int     `json:"totalJobs"`
	SuccessfulJobs int     `json:"successfulJobs"`
	ConvertedJobs  int     `json:"convertedJobs"`
	SuccessRate    float64 `json:"successRate"`
	ConversionRate float64 `json:"conversionRate"`
}

type PageInfo struct {
	Size          int `json:"size"`
	TotalElements int `json:"totalElements"`
	TotalPages    int `json:"totalPages"`
	Number        int `json:"number"`
}

type PagedResponse[T any] struct {
	Content []T      `json:"content"`
	Page    PageInfo `json:"page"`
}

type ListJobsParams struct {
	Type            string          `json:"type,omitempty"`
	ExecutionStatus ExecutionStatus `json:"executionStatus,omitempty"`
	OutcomeType     OutcomeType     `json:"outcomeType,omitempty"`
	StartDate       string          `json:"startDate,omitempty"`
	EndDate         string          `json:"endDate,omitempty"`
	Page            *int            `json:"page,omitempty"`
	Size            *int            `json:"size,omitempty"`
	Sort            string          `json:"sort,omitempty"`
}

type ConversionFunnelParams struct {
	StartDate string `json:"startDate,omitempty"`
	EndDate   string `json:"endDate,omitempty"`
	JobType   string `json:"jobType,omitempty"`
}
