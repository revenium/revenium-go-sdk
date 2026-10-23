package fal

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSubscribeWithNoSubmitAnswer_IsNotRecordedAsAFailedJob(t *testing.T) {
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
			return rejectingFal(t, http.StatusGatewayTimeout, `upstream timed out`), context.Background()
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
		"answer without a request id": func(t *testing.T) (string, context.Context) {
			return rejectingFal(t, http.StatusOK, `{"status":"IN_QUEUE"}`), context.Background()
		},
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			url, ctx := setup(t)
			client, mock := newFalAgainst(t, url)

			_, err := client.Subscribe(ctx, slowVideoEndpoint, map[string]interface{}{"prompt": "boat"}, nil)
			require.Error(t, err)
			client.Flush()

			assert.Empty(t, mock.GetPayloads(), "fal may have queued and be running the job, so it is not recorded as failed")
		})
	}
}

func TestSubscribe_JobStillUnknownAtCloseIsRecordedThen(t *testing.T) {
	queue := newSlowQueue(t)
	client, mock := newFalAgainst(t, queue.server.URL)

	abandonSubscribe(t, client, slowVideoEndpoint, map[string]interface{}{"prompt": "boat"}, context.Background())
	client.Flush()
	assert.Empty(t, mock.GetPayloads(), "the job is still being polled in the background")

	require.NoError(t, client.Close())
	payloads := mock.GetPayloads()
	require.Len(t, payloads, 1)
	assert.Equal(t, "req-slow", payloads[0]["transactionId"])
	assert.Contains(t, payloads[0]["errorReason"], "outcome is unknown")
}

func TestAbandonedJobWaitLimit_IsTheRequestTimeoutCappedAtTenMinutes(t *testing.T) {
	cases := map[string]struct {
		requestTimeout time.Duration
		want           time.Duration
	}{
		"default request timeout":   {0, 10 * time.Minute},
		"longer request timeout":    {time.Hour, 10 * time.Minute},
		"shorter request timeout":   {2 * time.Minute, 2 * time.Minute},
		"request timeout of 10 min": {10 * time.Minute, 10 * time.Minute},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			client := &ReveniumFal{config: &Config{RequestTimeout: tc.requestTimeout}}
			assert.Equal(t, tc.want, client.abandonedJobWaitLimit())
		})
	}
}

func shortenAbandonedWait(t *testing.T, limit time.Duration) {
	t.Helper()
	original := maxAbandonedWait
	maxAbandonedWait = limit
	t.Cleanup(func() { maxAbandonedWait = original })
}

func TestSubscribe_JobStillUnknownAtTheWaitLimitIsRecordedWithoutClose(t *testing.T) {
	shortenAbandonedWait(t, time.Second)
	queue := newSlowQueue(t)
	client, mock := newFalAgainst(t, queue.server.URL)
	client.config.RequestTimeout = time.Minute

	abandonSubscribe(t, client, slowVideoEndpoint, map[string]interface{}{"prompt": "boat"}, context.Background())

	require.True(t, mock.WaitForPayloads(1, 5*time.Second))
	assert.Contains(t, mock.GetPayloads()[0]["errorReason"], "req-slow had not finished")
}
