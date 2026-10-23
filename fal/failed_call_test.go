package fal

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/revenium/revenium-go-sdk/core/metering"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func rejectingFal(t *testing.T, status int, body string) string {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	return server.URL
}

func TestRejectedRequest_IsRecordedOnceAsAnError(t *testing.T) {
	cases := map[string]struct {
		status int
		body   string
		call   func(r *ReveniumFal) error
		op     string
	}{
		"run image": {http.StatusTooManyRequests, `{"detail":"Rate limit exceeded"}`, func(r *ReveniumFal) error {
			_, err := r.Run(context.Background(), "fal-ai/flux/dev", map[string]interface{}{"prompt": "say hello please"}, nil)
			return err
		}, "IMAGE"},
		"subscribe video": {http.StatusBadRequest, `{"detail":[{"loc":["body","prompt"],"msg":"field required","type":"value_error.missing"}]}`, func(r *ReveniumFal) error {
			_, err := r.Subscribe(context.Background(), slowVideoEndpoint, map[string]interface{}{}, nil)
			return err
		}, "VIDEO"},
		"stream": {http.StatusInternalServerError, `{"error":"internal"}`, func(r *ReveniumFal) error {
			_, err := r.Stream(context.Background(), "fal-ai/flux/dev", map[string]interface{}{"prompt": "x"}, nil)
			return err
		}, "IMAGE"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			client, mock := newFalAgainst(t, rejectingFal(t, tc.status, tc.body))

			require.Error(t, tc.call(client))
			client.Flush()

			require.True(t, mock.WaitForPayloads(1, 2*time.Second))
			payloads := mock.GetPayloads()
			require.Len(t, payloads, 1)
			record := payloads[0]
			assert.Equal(t, "ERROR", record["stopReason"])
			assert.Equal(t, float64(tc.status), record["errorCode"])
			assert.Equal(t, tc.op, record["operationType"])
			assert.Equal(t, "fal_ai", record["provider"])
			assert.NotContains(t, record["errorReason"], "say hello please")
			if tc.op == "IMAGE" {
				assert.Equal(t, float64(0), record["actualImageCount"], "fal does not bill a refused request")
			} else {
				assert.Equal(t, float64(0), record["durationSeconds"], "fal does not bill a refused request")
			}
		})
	}
}

func TestStreamThatBreaksOffAfterMeteringWasDisabled_IsStillRecordedOnce(t *testing.T) {
	breakOff := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"images\":[]}\n\n"))
		w.(http.Flusher).Flush()
		<-breakOff
		conn, _, err := w.(http.Hijacker).Hijack()
		if err == nil {
			_ = conn.Close()
		}
	}))
	t.Cleanup(server.Close)
	client, mock := newFalAgainst(t, server.URL)

	events, err := client.Stream(context.Background(), "fal-ai/flux/dev", map[string]interface{}{"prompt": "x"}, nil)
	require.NoError(t, err)
	first := <-events
	require.True(t, first.Partial)
	client.Disable()
	close(breakOff)
	var last StreamEvent
	for event := range events {
		last = event
	}
	require.Error(t, last.Error, "the stream broke off")
	client.Flush()

	require.True(t, mock.WaitForPayloads(1, 2*time.Second), "a stream that started with metering on is recorded however it ends")
	payloads := mock.GetPayloads()
	require.Len(t, payloads, 1)
	assert.Equal(t, "ERROR", payloads[0]["stopReason"])
	assert.Equal(t, true, payloads[0]["isStreamed"])
}

func TestRejectedRequest_PerCallCaptureOptOutBeatsTheClientsSetting(t *testing.T) {
	t.Setenv("REVENIUM_CAPTURE_ERROR_MESSAGES", "true")
	t.Setenv("REVENIUM_CAPTURE_PROMPTS", "")
	client, mock := newFalAgainst(t, rejectingFal(t, http.StatusBadRequest, `{"detail":"prompt 'say hello please' rejected"}`))
	client.config.CapturePrompts = true

	_, err := client.Run(context.Background(), "fal-ai/flux/dev", map[string]interface{}{"prompt": "say hello please"}, map[string]interface{}{"capturePrompts": false})
	require.Error(t, err)
	client.Flush()

	require.True(t, mock.WaitForPayloads(1, 2*time.Second))
	assert.Equal(t, "PROVIDER_ERROR (HTTP 400)", mock.GetPayloads()[0]["errorReason"])

	mock.Reset()
	_, err = client.Run(context.Background(), "fal-ai/flux/dev", map[string]interface{}{"prompt": "say hello please"}, nil)
	require.Error(t, err)
	client.Flush()
	require.True(t, mock.WaitForPayloads(1, 2*time.Second))
	assert.Contains(t, mock.GetPayloads()[0]["errorReason"], "say hello please", "with no per-call setting the client's opt-in applies")
}

func TestStreamCancelledWhileDeliveringAPart_IsRecordedOnce(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for i := 0; i < 40; i++ {
			_, _ = w.Write([]byte("data: {\"images\":[]}\n\n"))
		}
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	t.Cleanup(server.Close)
	client, mock := newFalAgainst(t, server.URL)
	ctx, cancel := context.WithCancel(context.Background())

	events, err := client.Stream(ctx, "fal-ai/flux/dev", map[string]interface{}{"prompt": "x"}, nil)
	require.NoError(t, err)
	require.Eventually(t, func() bool { return len(events) == cap(events) }, 2*time.Second, 10*time.Millisecond,
		"the consumer has stopped reading, so the reader blocks delivering a part")
	cancel()

	require.True(t, mock.WaitForPayloads(1, 2*time.Second), "a stream cancelled while delivering a part is recorded")
	for range events {
	}
	client.Flush()
	payloads := mock.GetPayloads()
	require.Len(t, payloads, 1)
	assert.Equal(t, "ERROR", payloads[0]["stopReason"])
	assert.Equal(t, metering.CallerCancelledReason, payloads[0]["errorReason"])
}

func TestFailedCall_IsRecordedUnderTheOperationTheEndpointsRepliesResolvedTo(t *testing.T) {
	var failing atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if failing.Load() {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"detail":"internal"}`))
			return
		}
		_, _ = w.Write([]byte(`{"video":{"url":"https://x/v.mp4","duration":5}}`))
	}))
	t.Cleanup(server.Close)
	client, mock := newFalAgainst(t, server.URL)
	const endpoint = "acme/custom-renderer"

	_, err := client.Run(context.Background(), endpoint, map[string]interface{}{"prompt": "x"}, nil)
	require.NoError(t, err)
	failing.Store(true)
	_, err = client.Run(context.Background(), endpoint, map[string]interface{}{"prompt": "x"}, nil)
	require.Error(t, err)
	client.Flush()

	require.True(t, mock.WaitForPayloads(2, 2*time.Second))
	payloads := mock.GetPayloads()
	require.Len(t, payloads, 2)
	assert.Equal(t, "VIDEO", payloads[0]["operationType"])
	assert.Equal(t, "VIDEO", payloads[1]["operationType"], "a failure lands under the same operation as the endpoint's successes")
	assert.Equal(t, "ERROR", payloads[1]["stopReason"])
}

func TestFailedVideoRecord_CarriesTheFieldsTheVideoEndpointAcceptsForAnErrorReport(t *testing.T) {
	client, mock := newFalAgainst(t, rejectingFal(t, http.StatusInternalServerError, `{"detail":"internal"}`))

	_, err := client.Run(context.Background(), slowVideoEndpoint, map[string]interface{}{"prompt": "x", "duration": "10"}, nil)
	require.Error(t, err)
	client.Flush()

	require.True(t, mock.WaitForPayloads(1, 2*time.Second))
	record := mock.GetPayloads()[0]
	keys := make([]string, 0, len(record))
	for key := range record {
		keys = append(keys, key)
	}
	assert.ElementsMatch(t, []string{
		"costType", "operationType", "operationSubtype", "model", "provider", "modelSource", "middlewareSource",
		"transactionId", "requestTime", "responseTime", "completionStartTime", "requestDuration", "timeToFirstToken", "isStreamed",
		"inputTokenCount", "outputTokenCount", "totalTokenCount", "reasoningTokenCount", "cacheCreationTokenCount", "cacheReadTokenCount",
		"stopReason", "errorReason", "errorCode", "completionStatus", "durationSeconds",
	}, keys)
	assert.Equal(t, float64(0), record["durationSeconds"], "required by the video endpoint; an error report skips its billing checks")
	assert.Equal(t, "FAILED", record["completionStatus"])
	assert.NotEmpty(t, record["errorReason"], "a non-blank errorReason is what makes the endpoint treat the record as an error report")
}
