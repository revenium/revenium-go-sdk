package openai

import (
	"context"
	"testing"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/revenium/revenium-go-sdk/core/jobs"
	"github.com/stretchr/testify/assert"
)

func TestChatRecordInsideAJobContextKeepsTheCallsOwnJobFields(t *testing.T) {
	unsetEnv(t, core.EnvAgenticJobID, core.EnvAgenticJobName)
	ctx := jobs.SetJobContext(context.Background(), &jobs.JobContextData{AgenticJobID: "context-job", AgenticJobType: "batch"})
	ctx = core.WithUsageMetadata(ctx, map[string]interface{}{"agenticJobId": "call-job", "agenticJobName": "call-name"})

	payload := meteredChatPayload(t, ctx)

	assert.Equal(t, "call-job", payload["agenticJobId"])
	assert.Equal(t, "call-name", payload["agenticJobName"])
	assert.Equal(t, "batch", payload["agenticJobType"])
}
