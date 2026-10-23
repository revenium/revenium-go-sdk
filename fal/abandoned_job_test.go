package fal

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

const slowVideoEndpoint = "fal-ai/kling-video/v1/standard/text-to-video"

type slowQueue struct {
	server      *httptest.Server
	finished    atomic.Bool
	submissions atomic.Int32
}

func newSlowQueue(t *testing.T) *slowQueue {
	t.Helper()
	queue := &slowQueue{}
	queue.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost:
			queue.submissions.Add(1)
			_, _ = w.Write([]byte(`{"request_id":"req-slow"}`))
		case strings.HasSuffix(r.URL.Path, "/requests/req-slow/status"):
			if queue.finished.Load() {
				_, _ = w.Write([]byte(`{"status":"COMPLETED","request_id":"req-slow"}`))
				return
			}
			_, _ = w.Write([]byte(`{"status":"IN_PROGRESS","request_id":"req-slow"}`))
		case strings.HasSuffix(r.URL.Path, "/requests/req-slow"):
			_, _ = w.Write([]byte(`{"video":{"url":"https://x/v.mp4","duration":5}}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(queue.server.Close)
	return queue
}

func newFalAgainst(t *testing.T, falURL string) (*ReveniumFal, *testutil.MockMeteringServer) {
	t.Helper()
	mock := testutil.NewMockMeteringServer()
	t.Cleanup(mock.Close)
	client, err := NewReveniumFal(&Config{
		FalAPIKey:      "test-key",
		FalBaseURL:     falURL,
		QueueBaseURL:   falURL,
		RequestTimeout: 10 * time.Second,
		Revenium:       &core.ReveniumConfig{APIKey: "hak_test_key_123", BaseURL: mock.URL()},
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })
	return client, mock
}

func TestSubscribe_ReturnsAtTheCallerDeadlineAndStillRecordsTheUnfinishedJob(t *testing.T) {
	queue := newSlowQueue(t)
	client, mock := newFalAgainst(t, queue.server.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	started := time.Now()
	_, err := client.Subscribe(ctx, slowVideoEndpoint, map[string]interface{}{"prompt": "boat", "duration": "10"}, nil)
	elapsed := time.Since(started)

	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Less(t, elapsed, 3500*time.Millisecond)
	require.NoError(t, client.Close())

	require.True(t, mock.WaitForPayloads(1, 2*time.Second))
	payloads := mock.GetPayloads()
	require.Len(t, payloads, 1)
	record := payloads[0]
	assert.Equal(t, int32(1), queue.submissions.Load())
	assert.Equal(t, "req-slow", record["transactionId"])
	assert.Equal(t, "VIDEO", record["operationType"])
	assert.Equal(t, "ERROR", record["stopReason"])
	assert.Contains(t, record["errorReason"], "req-slow had not finished")
	assert.EqualValues(t, 10, record["durationSeconds"])
}

func TestSubscribe_JobFinishedAfterTheCallerStoppedWaitingIsRecordedWithItsOutcome(t *testing.T) {
	queue := newSlowQueue(t)
	client, mock := newFalAgainst(t, queue.server.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := client.Subscribe(ctx, slowVideoEndpoint, map[string]interface{}{"prompt": "boat"}, nil)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	queue.finished.Store(true)

	require.True(t, mock.WaitForPayloads(1, 5*time.Second))
	record := mock.GetPayloads()[0]
	assert.Equal(t, "VIDEO", record["operationType"])
	assert.Equal(t, "END", record["stopReason"])
	assert.Nil(t, record["errorReason"])
	assert.EqualValues(t, 5, record["durationSeconds"])
}

func TestSubscribe_FailedJobIsRecordedAsAnErrorWithNoLength(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost {
			_, _ = w.Write([]byte(`{"request_id":"r1"}`))
			return
		}
		_, _ = w.Write([]byte(`{"status":"FAILED","request_id":"r1"}`))
	}))
	t.Cleanup(server.Close)
	client, mock := newFalAgainst(t, server.URL)

	_, err := client.Subscribe(context.Background(), slowVideoEndpoint, map[string]interface{}{}, nil)
	require.Error(t, err)
	require.NoError(t, client.Close())

	payloads := mock.GetPayloads()
	require.Len(t, payloads, 1)
	assert.Equal(t, "ERROR", payloads[0]["stopReason"])
	assert.Equal(t, "queue_request_failed", payloads[0]["errorReason"])
	assert.Equal(t, float64(0), payloads[0]["durationSeconds"], "fal does not bill a failed job")
}
