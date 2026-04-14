package runway

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func strPtr(s string) *string { return &s }

func TestMapRunwayStopReason(t *testing.T) {
	tests := []struct {
		name   string
		status TaskStatus
		err    *string
		want   string
	}{
		{"failed", TaskStatusFailed, nil, "ERROR"},
		{"canceled", TaskStatusCanceled, nil, "CANCELLED"},
		{"succeeded", TaskStatusSucceeded, nil, "END"},
		{"pending", TaskStatusPending, nil, "END"},
		{"with error string", TaskStatusSucceeded, strPtr("boom"), "ERROR"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, mapRunwayStopReason(tt.status, tt.err))
		})
	}
}

func TestBuildVideoMeteringPayload_BasicFields(t *testing.T) {
	result := &VideoGenerationResult{
		ID:       "task-1",
		Model:    "gen3a_turbo",
		Status:   TaskStatusSucceeded,
		Duration: 12 * time.Second,
		Metadata: map[string]interface{}{
			"duration":          float64(10),
			"requestedDuration": float64(10),
		},
		OutputURLs: []string{"https://x/out.mp4"},
	}
	payload := buildVideoMeteringPayload(result, nil, false, time.Now().Add(-12*time.Second))
	require.NotNil(t, payload)
	assert.Equal(t, "VIDEO", payload.OperationType)
	assert.Equal(t, "gen3a_turbo", payload.Model)
	assert.Equal(t, "RUNWAY", payload.ModelSource)
	assert.Equal(t, "task-1", payload.TransactionID)
	assert.Equal(t, "END", payload.StopReason)
}

func TestBuildVideoMeteringPayload_AppliesUsageMetadata(t *testing.T) {
	result := &VideoGenerationResult{
		ID: "task-2", Model: "m", Status: TaskStatusSucceeded,
		Metadata: map[string]interface{}{},
	}
	md := &UsageMetadata{
		OrganizationID: "org-1",
		ProductID:      "prod-1",
		TraceID:        "trace-1",
	}
	payload := buildVideoMeteringPayload(result, md, false, time.Now())
	require.NotNil(t, payload)
	assert.Equal(t, "org-1", payload.OrganizationID)
	assert.Equal(t, "prod-1", payload.ProductID)
	assert.Equal(t, "trace-1", payload.TraceID)
}

func TestUsageMetadataToMap_NilReturnsNil(t *testing.T) {
	assert.Nil(t, usageMetadataToMap(nil))
}

func TestUsageMetadataToMap_PopulatesAllFields(t *testing.T) {
	retry := 2
	score := 0.95
	md := &UsageMetadata{
		OrganizationID:       "org",
		ProductID:            "prod",
		TaskType:             "video-gen",
		Agent:                "agent-x",
		SubscriptionID:       "sub-1",
		TraceID:              "trace-1",
		ParentTransactionID:  "parent-1",
		TraceType:            "type-x",
		TraceName:            "name-x",
		Environment:          "prod",
		Region:               "us-east",
		RetryNumber:          &retry,
		CredentialAlias:      "creds-1",
		TaskID:               "task-1",
		ResponseQualityScore: &score,
		VideoJobID:           "vid-1",
		AudioJobID:           "aud-1",
		Custom:               map[string]interface{}{"customKey": "customVal"},
	}

	out := usageMetadataToMap(md)
	assert.Equal(t, "org", out["organizationId"])
	assert.Equal(t, "prod", out["productId"])
	assert.Equal(t, "video-gen", out["taskType"])
	assert.Equal(t, "trace-1", out["traceId"])
	assert.Equal(t, "name-x", out["traceName"])
	assert.Equal(t, "us-east", out["region"])
	assert.Equal(t, float64(2), out["retryNumber"])
	assert.Equal(t, 0.95, out["responseQualityScore"])
	assert.Equal(t, "vid-1", out["videoJobId"])
	assert.Equal(t, "customVal", out["customKey"])
}
