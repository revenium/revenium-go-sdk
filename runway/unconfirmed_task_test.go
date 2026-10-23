package runway

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestImageToVideo_TaskStillUnknownAtCloseIsRecordedThen(t *testing.T) {
	runway := newSlowRunway(t)
	client, mock := newRunwayAgainst(t, runway.server.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := client.ImageToVideo(ctx, &ImageToVideoRequest{PromptImage: "https://x/i.png", Model: "gen4_turbo", Duration: 10}, nil)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	client.Flush()
	assert.Empty(t, mock.GetPayloads(), "the task is still being polled in the background")

	require.NoError(t, client.Close())
	payloads := mock.GetPayloads()
	require.Len(t, payloads, 1)
	assert.Equal(t, "task-slow", payloads[0]["transactionId"])
	assert.Contains(t, payloads[0]["errorReason"], "outcome is unknown")
	assert.EqualValues(t, 10, payloads[0]["durationSeconds"])
}

func TestAbandonedTaskWaitLimit_IsThePollingTimeoutCappedAtTenMinutes(t *testing.T) {
	cases := map[string]struct {
		polling *PollingConfig
		want    time.Duration
	}{
		"default polling":         {nil, 10 * time.Minute},
		"shorter polling timeout": {&PollingConfig{MaxAttempts: 10, Timeout: 3 * time.Minute}, 3 * time.Minute},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			client := &ReveniumRunway{polling: tc.polling}
			assert.Equal(t, tc.want, client.abandonedTaskWaitLimit())
		})
	}
}

func shortenAbandonedWait(t *testing.T, limit time.Duration) {
	t.Helper()
	original := maxAbandonedWait
	maxAbandonedWait = limit
	t.Cleanup(func() { maxAbandonedWait = original })
}

func TestImageToVideo_TaskStillUnknownAtTheWaitLimitIsRecordedWithoutClose(t *testing.T) {
	shortenAbandonedWait(t, time.Second)
	runway := newSlowRunway(t)
	client, mock := newRunwayAgainst(t, runway.server.URL)
	client.polling = &PollingConfig{MaxAttempts: 100000, InitialInterval: 50 * time.Millisecond, MaxInterval: 50 * time.Millisecond, Timeout: time.Minute}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := client.ImageToVideo(ctx, &ImageToVideoRequest{PromptImage: "https://x/i.png", Model: "gen4_turbo", Duration: 10}, nil)
	require.ErrorIs(t, err, context.DeadlineExceeded)

	require.True(t, mock.WaitForPayloads(1, 5*time.Second))
	assert.Contains(t, mock.GetPayloads()[0]["errorReason"], "outcome is unknown")
}
