package runway

import (
	"context"
	"io"
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

func runwayAnswering(t *testing.T, create func(w http.ResponseWriter), taskStatus string) string {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasPrefix(r.URL.Path, "/v1/tasks/") {
			_, _ = w.Write([]byte(taskStatus))
			return
		}
		create(w)
	}))
	t.Cleanup(server.Close)
	return server.URL
}

func acceptTask(w http.ResponseWriter) {
	_, _ = w.Write([]byte(`{"id":"task-1","status":"PENDING"}`))
}

func onlyRunwayRecord(t *testing.T, client *ReveniumRunway, mock interface {
	GetPayloads() []map[string]interface{}
	WaitForPayloads(int, time.Duration) bool
}) map[string]interface{} {
	t.Helper()
	client.Flush()
	require.True(t, mock.WaitForPayloads(1, 2*time.Second))
	payloads := mock.GetPayloads()
	require.Len(t, payloads, 1)
	return payloads[0]
}

func TestRefusedTask_IsRecordedOnceAsAnError(t *testing.T) {
	refuse := func(w http.ResponseWriter) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"type":"rate_limit_error","message":"Too many requests for prompt 'say hello please'"}}`))
	}
	client, mock := newRunwayAgainst(t, runwayAnswering(t, refuse, ""))

	_, err := client.ImageToVideo(context.Background(), &ImageToVideoRequest{PromptImage: "https://x/i.png", Duration: 10}, nil)
	require.Error(t, err)

	record := onlyRunwayRecord(t, client, mock)
	assert.Equal(t, "ERROR", record["stopReason"])
	assert.Equal(t, "rate_limit_error (HTTP 429)", record["errorReason"])
	assert.Equal(t, float64(429), record["errorCode"])
	assert.Equal(t, float64(0), record["durationSeconds"])
	assert.Equal(t, float64(10), record["requestedDurationSeconds"])
}

func TestFailedTask_IsNamedByItsFailureCodeNotItsMessage(t *testing.T) {
	failed := `{"id":"task-1","status":"FAILED","error":"Your prompt 'say hello please' was flagged","failureCode":"SAFETY.INPUT.TEXT"}`
	client, mock := newRunwayAgainst(t, runwayAnswering(t, acceptTask, failed))

	_, err := client.ImageToVideo(context.Background(), &ImageToVideoRequest{PromptImage: "https://x/i.png"}, nil)
	require.Error(t, err)

	record := onlyRunwayRecord(t, client, mock)
	assert.Equal(t, "ERROR", record["stopReason"])
	assert.Equal(t, "SAFETY.INPUT.TEXT", record["errorReason"])
	assert.Equal(t, "FAILED", record["completionStatus"])
	assert.Equal(t, "task-1", record["transactionId"])
	assert.Equal(t, float64(0), record["durationSeconds"])
}

func TestCanceledTask_IsRecordedAsCancelledWithNoLength(t *testing.T) {
	client, mock := newRunwayAgainst(t, runwayAnswering(t, acceptTask, `{"id":"task-1","status":"CANCELED"}`))

	_, err := client.ImageToVideo(context.Background(), &ImageToVideoRequest{PromptImage: "https://x/i.png"}, nil)
	require.Error(t, err)

	record := onlyRunwayRecord(t, client, mock)
	assert.Equal(t, "CANCELLED", record["stopReason"])
	assert.Equal(t, "cancelled", record["errorReason"], "the video endpoint stores no stop reason, so the reason marks the cancellation")
	assert.Equal(t, "FAILED", record["completionStatus"], "the closest outcome the video endpoint stores")
	assert.Equal(t, float64(0), record["durationSeconds"])
}

func TestCanceledTask_DoesNotTakeTheIdsTheRetryOfTheCallWillCarry(t *testing.T) {
	client, mock := newRunwayAgainst(t, runwayAnswering(t, acceptTask, `{"id":"task-1","status":"CANCELED"}`))
	metadata := &UsageMetadata{Custom: map[string]interface{}{"transactionId": "tx-order-42", "idempotencyKey": "order-42"}}

	_, err := client.ImageToVideo(context.Background(), &ImageToVideoRequest{PromptImage: "https://x/i.png"}, metadata)
	require.Error(t, err)

	record := onlyRunwayRecord(t, client, mock)
	assert.Equal(t, "CANCELLED", record["stopReason"])
	assert.Equal(t, "task-1", record["transactionId"], "the record keeps the task's own id, not the one the retry will carry")
	require.NotEmpty(t, mock.GetRequests())
	assert.Regexp(t, `^order-42:err:[0-9a-f]{8}$`, mock.GetRequests()[0].Headers.Get("Idempotency-Key"))
}

func newRunwayCapturing(t *testing.T, runwayURL string, capturePrompts bool) (*ReveniumRunway, *testutil.MockMeteringServer) {
	t.Helper()
	mock := testutil.NewMockMeteringServer()
	t.Cleanup(mock.Close)
	client, err := NewReveniumRunway(&Config{
		RunwayAPIKey:   "test-key",
		RunwayBaseURL:  runwayURL,
		RunwayVersion:  "2024-11-06",
		RequestTimeout: 10 * time.Second,
		CapturePrompts: capturePrompts,
		Revenium:       &core.ReveniumConfig{APIKey: "hak_test_key_123", BaseURL: mock.URL()},
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })
	return client, mock
}

func TestFailedTask_MessageFollowsTheClientsPromptCaptureSetting(t *testing.T) {
	failed := `{"id":"task-1","status":"FAILED","error":"Your prompt 'say hello please' was flagged","failureCode":"SAFETY.INPUT.TEXT"}`
	cases := map[string]struct {
		envCapturePrompts string
		clientCapture     bool
		wantReason        string
	}{
		"client capture off beats the environment":  {"true", false, "SAFETY.INPUT.TEXT"},
		"client capture on without the environment": {"", true, "SAFETY.INPUT.TEXT: Your prompt 'say hello please' was flagged"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Setenv("REVENIUM_CAPTURE_ERROR_MESSAGES", "true")
			t.Setenv("REVENIUM_CAPTURE_PROMPTS", tc.envCapturePrompts)
			client, mock := newRunwayCapturing(t, runwayAnswering(t, acceptTask, failed), tc.clientCapture)

			_, err := client.ImageToVideo(context.Background(), &ImageToVideoRequest{PromptImage: "https://x/i.png"}, nil)
			require.Error(t, err)

			assert.Equal(t, tc.wantReason, onlyRunwayRecord(t, client, mock)["errorReason"])
		})
	}
}

func TestTaskThatFailsAfterTheCallerStoppedWaiting_IsRecordedOnceAsAnError(t *testing.T) {
	var failed atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case !strings.HasPrefix(r.URL.Path, "/v1/tasks/"):
			acceptTask(w)
		case failed.Load():
			_, _ = w.Write([]byte(`{"id":"task-1","status":"FAILED","failureCode":"INTERNAL.BAD_OUTPUT.CODE01"}`))
		default:
			_, _ = w.Write([]byte(`{"id":"task-1","status":"RUNNING"}`))
		}
	}))
	t.Cleanup(server.Close)
	client, mock := newRunwayAgainst(t, server.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := client.ImageToVideo(ctx, &ImageToVideoRequest{PromptImage: "https://x/i.png", Duration: 5}, nil)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	failed.Store(true)

	require.True(t, mock.WaitForPayloads(1, 5*time.Second))
	require.NoError(t, client.Close())
	payloads := mock.GetPayloads()
	require.Len(t, payloads, 1)
	assert.Equal(t, "ERROR", payloads[0]["stopReason"])
	assert.Equal(t, "INTERNAL.BAD_OUTPUT.CODE01", payloads[0]["errorReason"])
	assert.Equal(t, "task-1", payloads[0]["transactionId"])
	assert.Equal(t, float64(0), payloads[0]["durationSeconds"])
}

func TestFailedTask_PerCallCaptureSettingBeatsTheClients(t *testing.T) {
	failed := `{"id":"task-1","status":"FAILED","error":"Your prompt 'say hello please' was flagged","failureCode":"SAFETY.INPUT.TEXT"}`
	t.Setenv("REVENIUM_CAPTURE_ERROR_MESSAGES", "true")
	t.Setenv("REVENIUM_CAPTURE_PROMPTS", "")
	cases := map[string]struct {
		client  bool
		perCall interface{}
		reason  string
	}{
		"per-call false beats client true": {true, false, "SAFETY.INPUT.TEXT"},
		"per-call true beats client false": {false, true, "SAFETY.INPUT.TEXT: Your prompt 'say hello please' was flagged"},
		"unset uses the client setting":    {true, nil, "SAFETY.INPUT.TEXT: Your prompt 'say hello please' was flagged"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			client, mock := newRunwayCapturing(t, runwayAnswering(t, acceptTask, failed), tc.client)
			metadata := &UsageMetadata{}
			if tc.perCall != nil {
				metadata.Custom = map[string]interface{}{"capturePrompts": tc.perCall}
			}

			_, err := client.ImageToVideo(context.Background(), &ImageToVideoRequest{PromptImage: "https://x/i.png"}, metadata)
			require.Error(t, err)

			assert.Equal(t, tc.reason, onlyRunwayRecord(t, client, mock)["errorReason"])
		})
	}
}

func TestCreateWithNoAnswer_IsNotRecordedAsAFailedTask(t *testing.T) {
	cases := map[string]func(t *testing.T) (string, context.Context){
		"caller gave up before the answer": func(t *testing.T) (string, context.Context) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.Copy(io.Discard, r.Body)
				<-r.Context().Done()
			}))
			t.Cleanup(server.Close)
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			t.Cleanup(cancel)
			return server.URL, ctx
		},
		"gateway timeout": func(t *testing.T) (string, context.Context) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusGatewayTimeout)
			}))
			t.Cleanup(server.Close)
			return server.URL, context.Background()
		},
		"connection lost": func(t *testing.T) (string, context.Context) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				conn, _, err := w.(http.Hijacker).Hijack()
				if err == nil {
					_ = conn.Close()
				}
			}))
			t.Cleanup(server.Close)
			return server.URL, context.Background()
		},
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			url, ctx := setup(t)
			client, mock := newRunwayAgainst(t, url)

			_, err := client.ImageToVideo(ctx, &ImageToVideoRequest{PromptImage: "https://x/i.png", Duration: 10}, nil)
			require.Error(t, err)
			client.Flush()

			assert.Empty(t, mock.GetPayloads(), "Runway may have created and be rendering the task, so it is not recorded as failed")
		})
	}
}
