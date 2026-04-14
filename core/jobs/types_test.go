package jobs

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExecutionStatusValues(t *testing.T) {
	assert.Equal(t, ExecutionStatus("SUCCESS"), ExecutionStatusSuccess)
	assert.Equal(t, ExecutionStatus("FAILED"), ExecutionStatusFailed)
	assert.Equal(t, ExecutionStatus("CANCELLED"), ExecutionStatusCancelled)
}

func TestOutcomeTypeValues(t *testing.T) {
	assert.Equal(t, OutcomeType("CONVERTED"), OutcomeConverted)
	assert.Equal(t, OutcomeType("ESCALATED"), OutcomeEscalated)
	assert.Equal(t, OutcomeType("DEFLECTED"), OutcomeDeflected)
	assert.Equal(t, OutcomeType("UNSUCCESSFUL"), OutcomeUnsuccessful)
	assert.Equal(t, OutcomeType("CUSTOM"), OutcomeCustom)
}

func TestJobOutcomeJSONMarshal(t *testing.T) {
	val := 150.00
	outcome := JobOutcome{
		ExecutionStatus: ExecutionStatusSuccess,
		OutcomeType:     OutcomeConverted,
		OutcomeValue:    &val,
		OutcomeCurrency: "USD",
		Metadata:        `{"notes":"test"}`,
		ReportedBy:      "sdk-test",
	}

	data, err := json.Marshal(outcome)
	require.NoError(t, err)

	var parsed map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &parsed))

	assert.Equal(t, "SUCCESS", parsed["executionStatus"])
	assert.Equal(t, "CONVERTED", parsed["outcomeType"])
	assert.Equal(t, 150.0, parsed["outcomeValue"])
	assert.Equal(t, "USD", parsed["outcomeCurrency"])
	assert.Equal(t, "sdk-test", parsed["reportedBy"])
}

func TestJobOutcomeOmitsEmptyFields(t *testing.T) {
	outcome := JobOutcome{
		ExecutionStatus: ExecutionStatusFailed,
	}

	data, err := json.Marshal(outcome)
	require.NoError(t, err)

	var parsed map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &parsed))

	assert.Equal(t, "FAILED", parsed["executionStatus"])
	_, hasOutcomeType := parsed["outcomeType"]
	assert.False(t, hasOutcomeType)
	_, hasValue := parsed["outcomeValue"]
	assert.False(t, hasValue)
}

func TestJobResourceJSONUnmarshal(t *testing.T) {
	raw := `{
		"id": "JMwX9g4",
		"label": "Test Job",
		"resourceType": "job",
		"agenticJobId": "test-123",
		"source": "TELEMETRY",
		"hasOutcome": true,
		"executionStatus": "SUCCESS",
		"outcomeValue": 500.0
	}`

	var resource JobResource
	require.NoError(t, json.Unmarshal([]byte(raw), &resource))

	assert.Equal(t, "JMwX9g4", resource.ID)
	assert.Equal(t, "Test Job", resource.Label)
	assert.Equal(t, "test-123", resource.AgenticJobID)
	assert.Equal(t, "TELEMETRY", resource.Source)
	assert.True(t, resource.HasOutcome)
	require.NotNil(t, resource.ExecutionStatus)
	assert.Equal(t, "SUCCESS", *resource.ExecutionStatus)
	require.NotNil(t, resource.OutcomeValue)
	assert.Equal(t, 500.0, *resource.OutcomeValue)
	assert.Nil(t, resource.Name)
}

func TestJobROIResourceJSONUnmarshal(t *testing.T) {
	raw := `{
		"agenticJobId": "loan-app-123",
		"totalCost": 2.50,
		"outcomeValue": 500.0,
		"roi": 19900.0,
		"hasOutcome": true,
		"transactionCount": 15,
		"inputTokens": 5000,
		"outputTokens": 3000,
		"totalTokens": 8000
	}`

	var roi JobROIResource
	require.NoError(t, json.Unmarshal([]byte(raw), &roi))

	assert.Equal(t, "loan-app-123", roi.AgenticJobID)
	assert.Equal(t, 2.50, roi.TotalCost)
	require.NotNil(t, roi.OutcomeValue)
	assert.Equal(t, 500.0, *roi.OutcomeValue)
	require.NotNil(t, roi.ROI)
	assert.Equal(t, 19900.0, *roi.ROI)
	assert.True(t, roi.HasOutcome)
	assert.Equal(t, int64(15), roi.TransactionCount)
	assert.Equal(t, int64(8000), roi.TotalTokens)
}

func TestConversionFunnelResourceJSONUnmarshal(t *testing.T) {
	raw := `{
		"totalJobs": 100,
		"successfulJobs": 85,
		"convertedJobs": 60,
		"successRate": 0.85,
		"conversionRate": 0.71
	}`

	var funnel ConversionFunnelResource
	require.NoError(t, json.Unmarshal([]byte(raw), &funnel))

	assert.Equal(t, 100, funnel.TotalJobs)
	assert.Equal(t, 85, funnel.SuccessfulJobs)
	assert.Equal(t, 60, funnel.ConvertedJobs)
	assert.Equal(t, 0.85, funnel.SuccessRate)
	assert.Equal(t, 0.71, funnel.ConversionRate)
}

func TestPagedResponseGeneric(t *testing.T) {
	raw := `{
		"content": [
			{"id": "1", "label": "Job 1", "resourceType": "job", "agenticJobId": "j1", "source": "API", "hasOutcome": false},
			{"id": "2", "label": "Job 2", "resourceType": "job", "agenticJobId": "j2", "source": "TELEMETRY", "hasOutcome": true}
		],
		"page": {"size": 20, "totalElements": 2, "totalPages": 1, "number": 0}
	}`

	var paged PagedResponse[JobResource]
	require.NoError(t, json.Unmarshal([]byte(raw), &paged))

	assert.Len(t, paged.Content, 2)
	assert.Equal(t, "j1", paged.Content[0].AgenticJobID)
	assert.Equal(t, "j2", paged.Content[1].AgenticJobID)
	assert.Equal(t, 20, paged.Page.Size)
	assert.Equal(t, 2, paged.Page.TotalElements)
	assert.Equal(t, 1, paged.Page.TotalPages)
	assert.Equal(t, 0, paged.Page.Number)
}

func TestJobTimelineResourceJSONUnmarshal(t *testing.T) {
	raw := `{
		"transactions": [
			{
				"transactionId": "txn-001",
				"timestamp": "2026-03-05T10:00:00Z",
				"agent": "DocExtractor",
				"model": "gpt-4",
				"provider": "OpenAI",
				"duration": 5000,
				"cost": 0.0024,
				"inputTokens": 500,
				"outputTokens": 200,
				"totalTokens": 700,
				"status": "success"
			}
		],
		"totalCount": 1
	}`

	var timeline JobTimelineResource
	require.NoError(t, json.Unmarshal([]byte(raw), &timeline))

	assert.Equal(t, 1, timeline.TotalCount)
	require.Len(t, timeline.Transactions, 1)

	tx := timeline.Transactions[0]
	assert.Equal(t, "txn-001", tx.TransactionID)
	assert.Equal(t, "2026-03-05T10:00:00Z", tx.Timestamp)
	require.NotNil(t, tx.Agent)
	assert.Equal(t, "DocExtractor", *tx.Agent)
	require.NotNil(t, tx.Duration)
	assert.Equal(t, int64(5000), *tx.Duration)
}

func TestListJobsParamsOmitsEmpty(t *testing.T) {
	params := ListJobsParams{}
	data, err := json.Marshal(params)
	require.NoError(t, err)

	var parsed map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &parsed))

	assert.Empty(t, parsed)
}
