package jobs

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

type JobOutcome struct {
	ExecutionStatus ExecutionStatus `json:"executionStatus"`
	OutcomeType     OutcomeType     `json:"outcomeType,omitempty"`
	OutcomeValue    *float64        `json:"outcomeValue,omitempty"`
	OutcomeCurrency string          `json:"outcomeCurrency,omitempty"`
	Metadata        string          `json:"metadata,omitempty"`
	ReportedBy      string          `json:"reportedBy,omitempty"`
}

type JobResource struct {
	ID                string   `json:"id"`
	Label             string   `json:"label"`
	ResourceType      string   `json:"resourceType"`
	Created           *string  `json:"created,omitempty"`
	Updated           *string  `json:"updated,omitempty"`
	AgenticJobID      string   `json:"agenticJobId"`
	Name              *string  `json:"name,omitempty"`
	Type              *string  `json:"type,omitempty"`
	Version           *string  `json:"version,omitempty"`
	Source            string   `json:"source"`
	ExecutionStatus   *string  `json:"executionStatus,omitempty"`
	OutcomeType       *string  `json:"outcomeType,omitempty"`
	OutcomeValue      *float64 `json:"outcomeValue,omitempty"`
	OutcomeCurrency   *string  `json:"outcomeCurrency,omitempty"`
	OutcomeReportedAt *string  `json:"outcomeReportedAt,omitempty"`
	OutcomeMetadata   *string  `json:"outcomeMetadata,omitempty"`
	HasOutcome        bool     `json:"hasOutcome"`
}

type JobROIResource struct {
	AgenticJobID    string   `json:"agenticJobId"`
	AgenticJobName  *string  `json:"agenticJobName,omitempty"`
	AgenticJobType  *string  `json:"agenticJobType,omitempty"`
	TotalCost       float64  `json:"totalCost"`
	OutcomeValue    *float64 `json:"outcomeValue,omitempty"`
	OutcomeCurrency *string  `json:"outcomeCurrency,omitempty"`
	ROI             *float64 `json:"roi,omitempty"`
	ExecutionStatus *string  `json:"executionStatus,omitempty"`
	OutcomeType     *string  `json:"outcomeType,omitempty"`
	HasOutcome      bool     `json:"hasOutcome"`
	TransactionCount int64   `json:"transactionCount"`
	InputTokens     int64    `json:"inputTokens"`
	OutputTokens    int64    `json:"outputTokens"`
	TotalTokens     int64    `json:"totalTokens"`
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
	StartDate       string `json:"startDate,omitempty"`
	EndDate         string `json:"endDate,omitempty"`
	Page            *int   `json:"page,omitempty"`
	Size            *int   `json:"size,omitempty"`
	Sort            string `json:"sort,omitempty"`
}

type ConversionFunnelParams struct {
	StartDate string `json:"startDate,omitempty"`
	EndDate   string `json:"endDate,omitempty"`
	JobType   string `json:"jobType,omitempty"`
}
