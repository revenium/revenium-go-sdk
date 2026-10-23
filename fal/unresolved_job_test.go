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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type flakyQueue struct {
	server         *httptest.Server
	failingPolls   atomic.Int32
	failAlways     atomic.Bool
	finished       atomic.Bool
	resultFailures atomic.Int32
	output         string
}

func newFlakyQueue(t *testing.T, output string) *flakyQueue {
	t.Helper()
	queue := &flakyQueue{output: output}
	queue.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost:
			_, _ = w.Write([]byte(`{"request_id":"req-flaky"}`))
		case strings.HasSuffix(r.URL.Path, "/requests/req-flaky/status"):
			if queue.failAlways.Load() || queue.failingPolls.Add(-1) >= 0 {
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = w.Write([]byte(`upstream unavailable`))
				return
			}
			if queue.finished.Load() {
				_, _ = w.Write([]byte(`{"status":"COMPLETED"}`))
				return
			}
			_, _ = w.Write([]byte(`{"status":"IN_PROGRESS"}`))
		case strings.HasSuffix(r.URL.Path, "/requests/req-flaky"):
			if queue.resultFailures.Add(-1) >= 0 {
				w.WriteHeader(http.StatusBadGateway)
				return
			}
			_, _ = w.Write([]byte(queue.output))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(queue.server.Close)
	return queue
}

func abandonSubscribe(t *testing.T, client *ReveniumFal, endpoint string, input map[string]interface{}, ctx context.Context) {
	t.Helper()
	short, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()
	_, err := client.Subscribe(short, endpoint, input, nil)
	require.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestSubscribe_TemporaryFailuresAfterTheCallerLeftAreRetriedUntilTheOutcomeIsKnown(t *testing.T) {
	queue := newFlakyQueue(t, `{"video":{"url":"https://x/v.mp4","duration":5}}`)
	client, mock := newFalAgainst(t, queue.server.URL)

	abandonSubscribe(t, client, slowVideoEndpoint, map[string]interface{}{"prompt": "boat"}, context.Background())
	queue.failingPolls.Store(1)
	queue.resultFailures.Store(1)
	queue.finished.Store(true)

	require.True(t, mock.WaitForPayloads(1, 15*time.Second))
	record := mock.GetPayloads()[0]
	assert.Equal(t, "END", record["stopReason"])
	assert.EqualValues(t, 5, record["durationSeconds"])
}

func TestSubscribe_TemporaryFailureWhileTheCallerWaitsStillRecordsTheJob(t *testing.T) {
	queue := newFlakyQueue(t, `{"video":{"url":"https://x/v.mp4","duration":5}}`)
	queue.failingPolls.Store(1)
	queue.finished.Store(true)
	client, mock := newFalAgainst(t, queue.server.URL)

	_, err := client.Subscribe(context.Background(), slowVideoEndpoint, map[string]interface{}{"prompt": "boat"}, nil)
	require.Error(t, err)

	require.True(t, mock.WaitForPayloads(1, 5*time.Second))
	assert.Equal(t, "END", mock.GetPayloads()[0]["stopReason"])
}

func TestSubscribe_JobWhoseOutcomeNeverArrivesIsRecordedAsUnfinished(t *testing.T) {
	queue := newFlakyQueue(t, `{}`)
	client, mock := newFalAgainst(t, queue.server.URL)

	abandonSubscribe(t, client, slowVideoEndpoint, map[string]interface{}{"prompt": "boat", "duration": "10"}, context.Background())
	queue.failAlways.Store(true)
	time.Sleep(1500 * time.Millisecond)
	require.NoError(t, client.Close())

	payloads := mock.GetPayloads()
	require.Len(t, payloads, 1)
	assert.Equal(t, "ERROR", payloads[0]["stopReason"])
	assert.Contains(t, payloads[0]["errorReason"], "req-flaky had not finished")
}

func TestSubscribe_UnfinishedImageJobCarriesTheRequestedImageCount(t *testing.T) {
	cases := map[string]struct {
		input map[string]interface{}
		want  int
	}{
		"num_images set": {map[string]interface{}{"prompt": "cat", "num_images": 4}, 4},
		"default":        {map[string]interface{}{"prompt": "cat"}, 1},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			queue := newFlakyQueue(t, `{}`)
			client, mock := newFalAgainst(t, queue.server.URL)

			abandonSubscribe(t, client, "fal-ai/flux/dev", tc.input, context.Background())
			require.NoError(t, client.Close())

			payloads := mock.GetPayloads()
			require.Len(t, payloads, 1)
			assert.Equal(t, "IMAGE", payloads[0]["operationType"])
			assert.EqualValues(t, tc.want, payloads[0]["requestedImageCount"])
			assert.EqualValues(t, tc.want, payloads[0]["actualImageCount"])
		})
	}
}

func TestSubscribe_BackgroundRecordIgnoresChangesTheCallerMakesAfterReturning(t *testing.T) {
	queue := newFlakyQueue(t, `{"video":{"url":"https://x/v.mp4","duration":5}}`)
	client, mock := newFalAgainst(t, queue.server.URL)
	subscriber := map[string]interface{}{"email": "first@example.com"}
	ctx := core.WithUsageMetadata(context.Background(), map[string]interface{}{"subscriber": subscriber})

	abandonSubscribe(t, client, slowVideoEndpoint, map[string]interface{}{"prompt": "boat"}, ctx)
	subscriber["email"] = "next-request@example.com"
	queue.finished.Store(true)

	require.True(t, mock.WaitForPayloads(1, 5*time.Second))
	assert.Equal(t, map[string]interface{}{"email": "first@example.com"}, mock.GetPayloads()[0]["subscriber"])
}

func TestMeteredInputFields_KeepsOnlyScalarsTheRecordNeeds(t *testing.T) {
	input := map[string]interface{}{
		"image_url":  "data:image/png;base64,AAAA",
		"input":      map[string]interface{}{"prompt": "nested"},
		"duration":   "8s",
		"num_images": 2,
	}

	assert.Equal(t, map[string]interface{}{"prompt": "nested", "duration": "8s", "num_images": 2}, meteredInputFields(input))
}

func TestSubscribe_RecordsFromTheSameInputFieldsWhetherTheCallerWaitedOrNot(t *testing.T) {
	input := map[string]interface{}{"input": map[string]interface{}{"prompt": "boat"}, "duration": "10", "image_url": "https://x/in.png"}
	recordOf := func(abandon bool) map[string]interface{} {
		queue := newFlakyQueue(t, `{"video":{"url":"https://x/v.mp4"}}`)
		client, mock := newFalAgainst(t, queue.server.URL)
		client.config.CapturePrompts = true
		if abandon {
			abandonSubscribe(t, client, slowVideoEndpoint, input, context.Background())
			queue.finished.Store(true)
		} else {
			queue.finished.Store(true)
			_, err := client.Subscribe(context.Background(), slowVideoEndpoint, input, nil)
			require.NoError(t, err)
			client.Flush()
		}
		require.True(t, mock.WaitForPayloads(1, 5*time.Second))
		return mock.GetPayloads()[0]
	}

	waited, abandoned := recordOf(false), recordOf(true)

	for _, field := range []string{"durationSeconds", "requestedDurationSeconds", "inputMessages", "operationType", "model"} {
		assert.Equal(t, waited[field], abandoned[field], field)
	}
	assert.EqualValues(t, 10, waited["durationSeconds"])
}
