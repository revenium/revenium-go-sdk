package jobs

import (
	"context"
	"testing"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunWithJobContext_CallsCarryTheJob(t *testing.T) {
	data := &JobContextData{AgenticJobID: "job-1", AgenticJobName: "nightly-triage", AgenticJobType: "batch", AgenticJobVersion: "v3"}

	metadata, err := RunWithJobContext(context.Background(), data, func(ctx context.Context) (map[string]interface{}, error) {
		return core.GetUsageMetadata(ctx), nil
	})

	require.NoError(t, err)
	assert.Equal(t, map[string]interface{}{
		"agenticJobId":      "job-1",
		"agenticJobName":    "nightly-triage",
		"agenticJobType":    "batch",
		"agenticJobVersion": "v3",
	}, metadata)
}

func TestSetJobContext_EachJobFieldIsResolvedOnItsOwn(t *testing.T) {
	ctx := SetJobContext(context.Background(), &JobContextData{AgenticJobID: "job-1", AgenticJobName: "nightly"})
	ctx = core.WithUsageMetadata(ctx, map[string]interface{}{"agenticJobId": "job-2"})

	metadata := core.GetUsageMetadata(ctx)

	assert.Equal(t, "job-2", metadata["agenticJobId"])
	assert.Equal(t, "nightly", metadata["agenticJobName"])
}

func TestClearJobContext_RemovesTheJob(t *testing.T) {
	ctx := ClearJobContext(SetJobContext(context.Background(), &JobContextData{AgenticJobID: "job-1"}))

	assert.Nil(t, GetJobContext(ctx))
	assert.NotContains(t, core.GetUsageMetadata(ctx), "agenticJobId")
}

func TestSetJobContext_ABlankJobFieldNeverReplacesTheCallsValue(t *testing.T) {
	ctx := SetJobContext(context.Background(), &JobContextData{AgenticJobID: "job-1"})
	ctx = core.WithUsageMetadata(ctx, map[string]interface{}{"agenticJobName": "named-by-call"})

	metadata := core.GetUsageMetadata(ctx)

	assert.Equal(t, "job-1", metadata["agenticJobId"])
	assert.Equal(t, "named-by-call", metadata["agenticJobName"])
}

func TestSetJobContext_ANestedJobReplacesTheOuterJob(t *testing.T) {
	outer := SetJobContext(context.Background(), &JobContextData{AgenticJobID: "outer", AgenticJobName: "outer-name"})

	metadata := core.GetUsageMetadata(SetJobContext(outer, &JobContextData{AgenticJobID: "inner"}))

	assert.Equal(t, map[string]interface{}{"agenticJobId": "inner"}, metadata)
}

func TestSetJobContext_ClearsEveryFieldTheJobContextSets(t *testing.T) {
	outer := SetJobContext(context.Background(), &JobContextData{AgenticJobID: "outer", AgenticJobName: "n", AgenticJobType: "t", AgenticJobVersion: "v"})

	metadata := core.GetUsageMetadata(SetJobContext(outer, &JobContextData{}))

	for key := range (&JobContextData{AgenticJobID: "x"}).usageMetadata() {
		assert.NotContains(t, metadata, key)
	}
}
