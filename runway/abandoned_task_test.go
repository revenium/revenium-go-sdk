package runway

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/revenium/revenium-go-sdk/core/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type slowRunway struct {
	server    *httptest.Server
	finished  atomic.Bool
	creations atomic.Int32
}

func newSlowRunway(t *testing.T) *slowRunway {
	t.Helper()
	stub := &slowRunway{}
	stub.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if !strings.HasPrefix(r.URL.Path, "/v1/tasks/") {
			stub.creations.Add(1)
			_, _ = w.Write([]byte(`{"id":"task-slow","status":"PENDING"}`))
			return
		}
		if stub.finished.Load() {
			_, _ = w.Write([]byte(`{"id":"task-slow","status":"SUCCEEDED","output":["https://x/slow.mp4"]}`))
			return
		}
		_, _ = w.Write([]byte(`{"id":"task-slow","status":"RUNNING"}`))
	}))
	t.Cleanup(stub.server.Close)
	return stub
}

func newRunwayAgainst(t *testing.T, runwayURL string) (*ReveniumRunway, *testutil.MockMeteringServer) {
	t.Helper()
	mock := testutil.NewMockMeteringServer()
	t.Cleanup(mock.Close)
	client, err := NewReveniumRunway(&Config{
		RunwayAPIKey:   "test-key",
		RunwayBaseURL:  runwayURL,
		RunwayVersion:  "2024-11-06",
		RequestTimeout: 10 * time.Second,
		Revenium:       &core.ReveniumConfig{APIKey: "hak_test_key_123", BaseURL: mock.URL()},
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })
	return client, mock
}

func TestImageToVideo_ReturnsAtTheCallerDeadlineAndStillRecordsTheUnfinishedTask(t *testing.T) {
	runway := newSlowRunway(t)
	client, mock := newRunwayAgainst(t, runway.server.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	started := time.Now()
	_, err := client.ImageToVideo(ctx, &ImageToVideoRequest{PromptImage: "https://x/i.png", Model: "gen4_turbo", Duration: 10}, nil)
	elapsed := time.Since(started)

	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Less(t, elapsed, 3500*time.Millisecond, "the wait must end at the caller's deadline, not after the next poll pause")
	require.NoError(t, client.Close())

	require.True(t, mock.WaitForPayloads(1, 2*time.Second))
	payloads := mock.GetPayloads()
	require.Len(t, payloads, 1)
	record := payloads[0]
	assert.Equal(t, int32(1), runway.creations.Load())
	assert.Equal(t, "task-slow", record["transactionId"])
	assert.Equal(t, "ERROR", record["stopReason"])
	assert.Contains(t, record["errorReason"], "task-slow had not finished")
	assert.EqualValues(t, 10, record["durationSeconds"])
}

func TestImageToVideo_TaskFinishedAfterTheCallerStoppedWaitingIsRecordedWithItsOutcome(t *testing.T) {
	runway := newSlowRunway(t)
	client, mock := newRunwayAgainst(t, runway.server.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := client.ImageToVideo(ctx, &ImageToVideoRequest{PromptImage: "https://x/i.png", Model: "gen4_turbo", Duration: 5}, nil)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	runway.finished.Store(true)

	require.True(t, mock.WaitForPayloads(1, 5*time.Second))
	record := mock.GetPayloads()[0]
	assert.Equal(t, "task-slow", record["transactionId"])
	assert.Equal(t, "END", record["stopReason"])
	assert.Nil(t, record["errorReason"])
	assert.EqualValues(t, 5, record["durationSeconds"])
}

func TestImageToVideo_TaskStillRunningWhenPollingGivesUpIsRecordedAsUnfinished(t *testing.T) {
	runway := newSlowRunway(t)
	client, mock := newRunwayAgainst(t, runway.server.URL)
	client.polling = &PollingConfig{MaxAttempts: 1, InitialInterval: time.Millisecond, MaxInterval: time.Millisecond, Timeout: time.Minute}

	_, err := client.ImageToVideo(context.Background(), &ImageToVideoRequest{PromptImage: "https://x/i.png", Model: "gen4_turbo"}, nil)
	require.True(t, IsTaskError(err))
	client.Flush()

	require.True(t, mock.WaitForPayloads(1, 2*time.Second))
	record := mock.GetPayloads()[0]
	assert.Equal(t, "ERROR", record["stopReason"])
	assert.Contains(t, record["errorReason"], "task-slow had not finished")
}

func TestImageToVideo_FailedTaskIsRecordedAsAnErrorWithNoLength(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasPrefix(r.URL.Path, "/v1/tasks/") {
			_, _ = w.Write([]byte(`{"id":"task-f","status":"FAILED","error":"bad input"}`))
			return
		}
		_, _ = w.Write([]byte(`{"id":"task-f","status":"PENDING"}`))
	}))
	t.Cleanup(server.Close)
	client, mock := newRunwayAgainst(t, server.URL)

	_, err := client.ImageToVideo(context.Background(), &ImageToVideoRequest{PromptImage: "https://x/i.png"}, nil)
	require.True(t, IsTaskError(err))
	require.NoError(t, client.Close())

	payloads := mock.GetPayloads()
	require.Len(t, payloads, 1)
	assert.Equal(t, "ERROR", payloads[0]["stopReason"])
	assert.Equal(t, "task-f", payloads[0]["transactionId"])
	assert.Equal(t, float64(0), payloads[0]["durationSeconds"], "Runway does not bill a failed task")
}

func TestWaitForTaskCompletion_ReturnsWhenTheContextEndsDuringAPollPause(t *testing.T) {
	runway := newSlowRunway(t)
	client := NewRunwayClient(&Config{RunwayAPIKey: "k", RunwayBaseURL: runway.server.URL})
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	started := time.Now()
	status, err := client.WaitForTaskCompletion(ctx, "task-slow", &PollingConfig{
		MaxAttempts: 10, InitialInterval: 10 * time.Second, MaxInterval: 10 * time.Second, Timeout: time.Minute,
	})

	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Nil(t, status)
	assert.Less(t, time.Since(started), time.Second)
}

func TestImageToVideo_BackgroundRecordIgnoresChangesTheCallerMakesAfterReturning(t *testing.T) {
	runway := newSlowRunway(t)
	client, mock := newRunwayAgainst(t, runway.server.URL)
	subscriber := map[string]interface{}{"email": "first@example.com"}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := client.ImageToVideo(ctx, &ImageToVideoRequest{PromptImage: "https://x/i.png", Model: "gen4_turbo"}, &UsageMetadata{Subscriber: subscriber})
	require.ErrorIs(t, err, context.DeadlineExceeded)
	subscriber["email"] = "next-request@example.com"
	runway.finished.Store(true)

	require.True(t, mock.WaitForPayloads(1, 5*time.Second))
	assert.Equal(t, map[string]interface{}{"email": "first@example.com"}, mock.GetPayloads()[0]["subscriber"])
}
