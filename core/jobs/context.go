package jobs

import (
	"context"

	"github.com/revenium/revenium-go-sdk/core"
)

type contextKey string

const jobContextKey contextKey = "revenium_job_context"

type JobContextData struct {
	AgenticJobID      string
	AgenticJobName    string
	AgenticJobType    string
	AgenticJobVersion string
}

var jobMetadataKeys = func() []string {
	var keys []string
	for key := range (&JobContextData{}).usageMetadata() {
		keys = append(keys, key)
	}
	return keys
}()

// SetJobContext attaches data to ctx, replacing any job set on a parent
// context. Each job field is resolved on its own for every metered call made
// with the returned context: the call's own usage metadata wins, then data,
// then REVENIUM_AGENTIC_JOB_* in the environment. A blank field in data sets
// nothing.
func SetJobContext(ctx context.Context, data *JobContextData) context.Context {
	withoutJob := core.WithoutDefaultUsageMetadata(ctx, jobMetadataKeys...)
	return core.WithDefaultUsageMetadata(context.WithValue(withoutJob, jobContextKey, data), data.usageMetadata())
}

func GetJobContext(ctx context.Context) *JobContextData {
	if data, ok := ctx.Value(jobContextKey).(*JobContextData); ok {
		return data
	}
	return nil
}

func ClearJobContext(ctx context.Context) context.Context {
	return SetJobContext(ctx, nil)
}

func (d *JobContextData) usageMetadata() map[string]interface{} {
	if d == nil {
		return nil
	}
	return map[string]interface{}{
		"agenticJobId":      d.AgenticJobID,
		"agenticJobName":    d.AgenticJobName,
		"agenticJobType":    d.AgenticJobType,
		"agenticJobVersion": d.AgenticJobVersion,
	}
}

// RunWithJobContext runs fn with ctx attached to the job, so the calls fn
// makes are recorded against it.
func RunWithJobContext[T any](ctx context.Context, data *JobContextData, fn func(context.Context) (T, error)) (T, error) {
	return fn(SetJobContext(ctx, data))
}
