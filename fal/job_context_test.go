package fal

import (
	"context"
	"testing"
	"time"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/revenium/revenium-go-sdk/core/jobs"
	"github.com/revenium/revenium-go-sdk/core/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunInsideAJobContextKeepsTheJobFieldsPassedForTheCall(t *testing.T) {
	clearOrganizationAndProductEnv(t)
	for _, name := range []string{core.EnvAgenticJobID, core.EnvAgenticJobName} {
		t.Setenv(name, "")
	}
	fal := imageServer(t)
	metering := testutil.NewMockMeteringServer()
	t.Cleanup(metering.Close)
	client, err := NewReveniumFal(&Config{
		FalAPIKey:      "test-key",
		FalBaseURL:     fal.URL,
		QueueBaseURL:   fal.URL,
		RequestTimeout: 10 * time.Second,
		Revenium:       &core.ReveniumConfig{APIKey: "hak_test_key_123", BaseURL: metering.URL()},
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })
	ctx := jobs.SetJobContext(context.Background(), &jobs.JobContextData{AgenticJobID: "context-job", AgenticJobType: "batch"})

	record := runAndCaptureRecord(t, ctx, client, metering, map[string]interface{}{"agenticJobId": "call-job", "agenticJobName": "call-name"})

	assert.Equal(t, "call-job", record["agenticJobId"])
	assert.Equal(t, "call-name", record["agenticJobName"], "a field the job context leaves blank never replaces the call's value")
	assert.Equal(t, "batch", record["agenticJobType"])
}
