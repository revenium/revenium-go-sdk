package openai

import (
	"context"
	"testing"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/revenium/revenium-go-sdk/core/jobs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChatRecordInsideRunWithJobContextCarriesTheJob(t *testing.T) {
	unsetEnv(t, core.EnvAgenticJobID, core.EnvAgenticJobName)

	payload, err := jobs.RunWithJobContext(context.Background(), &jobs.JobContextData{AgenticJobID: "job-1", AgenticJobName: "nightly-triage"},
		func(ctx context.Context) (map[string]interface{}, error) {
			return meteredChatPayload(t, ctx), nil
		})

	require.NoError(t, err)
	assert.Equal(t, "job-1", payload["agenticJobId"])
	assert.Equal(t, "nightly-triage", payload["agenticJobName"])
}

func TestChatRecordCarriesTheJobAndTicketFromTheEnvironment(t *testing.T) {
	t.Setenv(core.EnvAgenticJobID, "invoice-batch-7")
	t.Setenv(core.EnvTicketID, "ENG-42")

	payload := meteredChatPayload(t, context.Background())

	assert.Equal(t, "invoice-batch-7", payload["agenticJobId"])
	assert.Equal(t, "ENG-42", payload["ticketId"])
}
