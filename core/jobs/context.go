package jobs

import "context"

type contextKey string

const jobContextKey contextKey = "revenium_job_context"

type JobContextData struct {
	AgenticJobID      string
	AgenticJobName    string
	AgenticJobType    string
	AgenticJobVersion string
}

func SetJobContext(ctx context.Context, data *JobContextData) context.Context {
	return context.WithValue(ctx, jobContextKey, data)
}

func GetJobContext(ctx context.Context) *JobContextData {
	if data, ok := ctx.Value(jobContextKey).(*JobContextData); ok {
		return data
	}
	return nil
}

func ClearJobContext(ctx context.Context) context.Context {
	return context.WithValue(ctx, jobContextKey, (*JobContextData)(nil))
}

func RunWithJobContext[T any](ctx context.Context, data *JobContextData, fn func(context.Context) (T, error)) (T, error) {
	return fn(SetJobContext(ctx, data))
}
