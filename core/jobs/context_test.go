package jobs

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSetAndGetJobContext(t *testing.T) {
	ctx := context.Background()
	data := &JobContextData{
		AgenticJobID:      "job-123",
		AgenticJobName:    "Loan Processing",
		AgenticJobType:    "loan",
		AgenticJobVersion: "1.0",
	}

	ctx = SetJobContext(ctx, data)
	result := GetJobContext(ctx)

	require.NotNil(t, result)
	assert.Equal(t, "job-123", result.AgenticJobID)
	assert.Equal(t, "Loan Processing", result.AgenticJobName)
	assert.Equal(t, "loan", result.AgenticJobType)
	assert.Equal(t, "1.0", result.AgenticJobVersion)
}

func TestGetJobContextReturnsNilForEmptyContext(t *testing.T) {
	ctx := context.Background()
	result := GetJobContext(ctx)
	assert.Nil(t, result)
}

func TestClearJobContext(t *testing.T) {
	ctx := context.Background()
	ctx = SetJobContext(ctx, &JobContextData{AgenticJobID: "job-123"})

	ctx = ClearJobContext(ctx)
	result := GetJobContext(ctx)
	assert.Nil(t, result)
}

func TestRunWithJobContext(t *testing.T) {
	ctx := context.Background()
	data := &JobContextData{
		AgenticJobID:   "run-job-456",
		AgenticJobName: "Test Run",
	}

	result, err := RunWithJobContext(ctx, data, func(innerCtx context.Context) (string, error) {
		jc := GetJobContext(innerCtx)
		require.NotNil(t, jc)
		return jc.AgenticJobID, nil
	})

	require.NoError(t, err)
	assert.Equal(t, "run-job-456", result)
}

func TestRunWithJobContextPreservesOuterContext(t *testing.T) {
	ctx := context.Background()
	outerData := &JobContextData{AgenticJobID: "outer-job"}
	ctx = SetJobContext(ctx, outerData)

	innerData := &JobContextData{AgenticJobID: "inner-job"}
	_, _ = RunWithJobContext(ctx, innerData, func(innerCtx context.Context) (bool, error) {
		jc := GetJobContext(innerCtx)
		require.NotNil(t, jc)
		assert.Equal(t, "inner-job", jc.AgenticJobID)
		return true, nil
	})

	outerResult := GetJobContext(ctx)
	require.NotNil(t, outerResult)
	assert.Equal(t, "outer-job", outerResult.AgenticJobID)
}

func TestRunWithJobContextPropagatesError(t *testing.T) {
	ctx := context.Background()
	data := &JobContextData{AgenticJobID: "err-job"}

	_, err := RunWithJobContext(ctx, data, func(_ context.Context) (string, error) {
		return "", assert.AnError
	})

	assert.Error(t, err)
	assert.Equal(t, assert.AnError, err)
}
