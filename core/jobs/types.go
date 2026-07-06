package jobs

import "fmt"

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

type OutcomeAmendConflictError struct {
	JobID   string
	Message string
}

func (e *OutcomeAmendConflictError) Error() string {
	return e.Message
}

type JobOutcomeAmendment struct {
	Reason          string          `json:"reason"`
	ExecutionStatus ExecutionStatus `json:"executionStatus,omitempty"`
	OutcomeType     OutcomeType     `json:"outcomeType,omitempty"`
	OutcomeValue    *float64        `json:"outcomeValue,omitempty"`
	OutcomeCurrency string          `json:"outcomeCurrency,omitempty"`
	Metadata        string          `json:"metadata,omitempty"`
	ReportedBy      string          `json:"reportedBy,omitempty"`
}

type JobOutcomeAmendmentEntry struct {
	AmendmentSequence int             `json:"amendmentSequence"`
	ExecutionStatus   ExecutionStatus `json:"executionStatus"`
	OutcomeType       *OutcomeType    `json:"outcomeType,omitempty"`
	OutcomeValue      *float64        `json:"outcomeValue,omitempty"`
	OutcomeCurrency   *string         `json:"outcomeCurrency,omitempty"`
	OutcomeMetadata   *string         `json:"outcomeMetadata,omitempty"`
	ReportedBy        *string         `json:"reportedBy,omitempty"`
	ReportedAt        string          `json:"reportedAt"`
	Reason            *string         `json:"reason,omitempty"`
}

type conflictResponseBody struct {
	Error          string `json:"error"`
	Guidance       string `json:"guidance"`
	ReportedAt     string `json:"reportedAt"`
	AmendmentCount int    `json:"amendmentCount"`
}

func newOutcomeAlreadyReportedError(jobID string, body *conflictResponseBody) *OutcomeAlreadyReportedError {
	msg := fmt.Sprintf("outcome already reported for job %s", jobID)
	if body.Error != "" {
		msg = body.Error
	}
	var reportedAt *string
	if body.ReportedAt != "" {
		reportedAt = &body.ReportedAt
	}
	return &OutcomeAlreadyReportedError{
		JobID:          jobID,
		ReportedAt:     reportedAt,
		AmendmentCount: body.AmendmentCount,
		Message:        msg,
	}
}

type JobOutcome struct {
	ExecutionStatus ExecutionStatus `json:"executionStatus"`
	OutcomeType     OutcomeType     `json:"outcomeType,omitempty"`
	OutcomeValue    *float64        `json:"outcomeValue,omitempty"`
	OutcomeCurrency string          `json:"outcomeCurrency,omitempty"`
	Metadata        string          `json:"metadata,omitempty"`
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
	HasOutcome            bool     `json:"hasOutcome"`
	OutcomeAmendmentCount *int     `json:"outcomeAmendmentCount,omitempty"`
	OutcomeUpdatedAt      *string  `json:"outcomeUpdatedAt,omitempty"`
	OutcomeUpdatedBy      *string  `json:"outcomeUpdatedBy,omitempty"`
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
