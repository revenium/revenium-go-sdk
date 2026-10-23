package google

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/revenium/revenium-go-sdk/core/metering"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/genai"
)

const (
	geminiVeoModel     = "veo-3.1-generate-preview"
	geminiVeoOperation = "models/veo-3.1-generate-preview/operations/op1"
	vertexVeoModel     = "veo-3.0-generate-001"
	vertexVeoOperation = "projects/p1/locations/us-central1/publishers/google/models/veo-3.0-generate-001/operations/op1"
)

type veoStub struct {
	operation string
	done      string
	submit    string
	polls     atomic.Int32
}

func geminiVeo(samples int) *veoStub {
	generated := strings.TrimSuffix(strings.Repeat(`{"video":{"uri":"https://generativelanguage.googleapis.com/v1beta/files/abc:download"}},`, samples), ",")
	return &veoStub{
		operation: geminiVeoOperation,
		submit:    `{"name":"` + geminiVeoOperation + `"}`,
		done:      `{"name":"` + geminiVeoOperation + `","done":true,"response":{"@type":"type.googleapis.com/google.ai.generativelanguage.v1beta.PredictLongRunningResponse","generateVideoResponse":{"generatedSamples":[` + generated + `]}}}`,
	}
}

func vertexVeo() *veoStub {
	return &veoStub{
		operation: vertexVeoOperation,
		submit:    `{"name":"` + vertexVeoOperation + `"}`,
		done:      `{"name":"` + vertexVeoOperation + `","done":true,"response":{"@type":"type.googleapis.com/cloud.ai.large_models.vision.GenerateVideoResponse","videos":[{"gcsUri":"gs://bucket/sample_0.mp4","mimeType":"video/mp4"}]}}`,
	}
}

func failedVeo() *veoStub {
	stub := geminiVeo(0)
	stub.done = `{"name":"` + geminiVeoOperation + `","done":true,"error":{"code":3,"message":"prompt rejected"}}`
	return stub
}

func (s *veoStub) handle(w http.ResponseWriter, r *http.Request) {
	switch {
	case strings.HasSuffix(r.URL.Path, ":predictLongRunning"):
		writeJSON(w, http.StatusOK, s.submit)
	case strings.HasSuffix(r.URL.Path, ":fetchPredictOperation"), strings.Contains(r.URL.Path, "/operations/"):
		if s.polls.Add(1) == 1 {
			writeJSON(w, http.StatusOK, `{"name":"`+s.operation+`"}`)
			return
		}
		writeJSON(w, http.StatusOK, s.done)
	default:
		http.NotFound(w, r)
	}
}

func fastVideoPolling(t *testing.T) {
	t.Helper()
	previous := videoPollInterval
	videoPollInterval = time.Millisecond
	t.Cleanup(func() { videoPollInterval = previous })
}

func pollLikeGoogleDocs(t *testing.T, c *stubbedClient, op *genai.GenerateVideosOperation) *genai.GenerateVideosOperation {
	t.Helper()
	for !op.Done {
		var err error
		op, err = c.client.GetGenaiClient().Operations.GetVideosOperation(context.Background(), op, nil)
		require.NoError(t, err)
	}
	return op
}

func secondsOf(durationSeconds int32) *genai.GenerateVideosConfig {
	return &genai.GenerateVideosConfig{DurationSeconds: &durationSeconds}
}

func assertVideoRecord(t *testing.T, record map[string]interface{}, model string, seconds float64) {
	t.Helper()
	assert.Equal(t, "VIDEO", record["operationType"])
	assert.Equal(t, model, record["model"])
	assert.Equal(t, "END", record["stopReason"])
	assert.Equal(t, seconds, record["durationSeconds"])
	assert.Equal(t, seconds, record["requestedDurationSeconds"])
}

func TestVeoPolledWithGoogleOperationsIsRecordedOnce(t *testing.T) {
	cases := []struct {
		provider    Provider
		model       string
		recordModel string
		stub        *veoStub
	}{
		{ProviderGoogleAI, geminiVeoModel, geminiVeoModel, geminiVeo(1)},
		{ProviderVertexAI, vertexVeoModel, "vertex_ai/" + vertexVeoModel, vertexVeo()},
	}
	for _, tc := range cases {
		t.Run(tc.provider.String(), func(t *testing.T) {
			c := newStubbedClient(t, tc.provider, tc.stub.handle)
			ctx := core.WithUsageMetadata(context.Background(), map[string]interface{}{"traceId": "veo-trace"})

			op, err := c.client.Models().GenerateVideo(ctx, tc.model, "a cat surfing", nil, secondsOf(6))
			require.NoError(t, err)
			done := pollLikeGoogleDocs(t, c, op)
			_, err = c.client.GetGenaiClient().Operations.GetVideosOperation(context.Background(), done, nil)
			require.NoError(t, err)

			require.Len(t, done.Response.GeneratedVideos, 1)
			assert.GreaterOrEqual(t, tc.stub.polls.Load(), int32(2), "the caller polled through Google's operations API")
			record := singleRecord(t, c)
			assertVideoRecord(t, record, tc.recordModel, 6)
			assert.Equal(t, "veo-trace", record["traceId"])
		})
	}
}

func TestVeoFromSourcePolledWithGoogleOperationsIsRecordedOnce(t *testing.T) {
	c := newStubbedClient(t, ProviderGoogleAI, geminiVeo(1).handle)

	op, err := c.client.Models().GenerateVideoFromSource(context.Background(), geminiVeoModel, &genai.GenerateVideosSource{Prompt: "a cat surfing"}, nil)
	require.NoError(t, err)
	pollLikeGoogleDocs(t, c, op)

	assertVideoRecord(t, singleRecord(t, c), geminiVeoModel, veoDefaultClipSeconds)
}

func TestVeoWaitForVideoIsRecordedOnce(t *testing.T) {
	fastVideoPolling(t)
	c := newStubbedClient(t, ProviderGoogleAI, geminiVeo(1).handle)

	op, err := c.client.Models().GenerateVideo(context.Background(), geminiVeoModel, "a cat surfing", nil, nil)
	require.NoError(t, err)
	done, err := c.client.Models().WaitForVideo(context.Background(), geminiVeoModel, op, "generation", nil)
	require.NoError(t, err)
	_, err = c.client.GetGenaiClient().Operations.GetVideosOperation(context.Background(), done, nil)
	require.NoError(t, err)

	assertVideoRecord(t, singleRecord(t, c), geminiVeoModel, veoDefaultClipSeconds)
}

func TestVeoWaitForVideoRecordsOperationSubmittedElsewhere(t *testing.T) {
	fastVideoPolling(t)
	c := newStubbedClient(t, ProviderGoogleAI, geminiVeo(1).handle)

	op, err := c.client.GetGenaiClient().Models.GenerateVideos(context.Background(), geminiVeoModel, "a cat surfing", nil, nil)
	require.NoError(t, err)
	_, err = c.client.Models().WaitForVideo(context.Background(), geminiVeoModel, op, "generation", secondsOf(4))
	require.NoError(t, err)

	assertVideoRecord(t, singleRecord(t, c), geminiVeoModel, 4)
}

func TestVeoDurationCoversEveryGeneratedVideo(t *testing.T) {
	c := newStubbedClient(t, ProviderGoogleAI, geminiVeo(2).handle)
	config := secondsOf(6)
	config.NumberOfVideos = 2

	op, err := c.client.Models().GenerateVideo(context.Background(), geminiVeoModel, "a cat surfing", nil, config)
	require.NoError(t, err)
	pollLikeGoogleDocs(t, c, op)

	assertVideoRecord(t, singleRecord(t, c), geminiVeoModel, 12)
}

func TestVeoOperationAlreadyDoneAtSubmitIsRecordedOnce(t *testing.T) {
	fastVideoPolling(t)
	stub := geminiVeo(1)
	stub.submit = stub.done
	c := newStubbedClient(t, ProviderGoogleAI, stub.handle)

	op, err := c.client.Models().GenerateVideo(context.Background(), geminiVeoModel, "a cat surfing", nil, secondsOf(8))
	require.NoError(t, err)
	require.True(t, op.Done)
	_, err = c.client.Models().WaitForVideo(context.Background(), geminiVeoModel, op, "generation", secondsOf(8))
	require.NoError(t, err)

	assertVideoRecord(t, singleRecord(t, c), geminiVeoModel, 8)
}

func TestVeoFailedOperationIsRecordedOnceAsError(t *testing.T) {
	fastVideoPolling(t)
	c := newStubbedClient(t, ProviderGoogleAI, failedVeo().handle)

	op, err := c.client.Models().GenerateVideo(context.Background(), geminiVeoModel, "a cat surfing", nil, nil)
	require.NoError(t, err)
	done := pollLikeGoogleDocs(t, c, op)
	require.NotNil(t, done.Error)
	_, err = c.client.Models().WaitForVideo(context.Background(), geminiVeoModel, done, "generation", nil)
	require.Error(t, err)

	record := singleRecord(t, c)
	assert.Equal(t, "ERROR", record["stopReason"])
	assert.Equal(t, "video_generation_failed", record["errorReason"], "the provider's message can quote the prompt and is not sent by default")
	assert.Equal(t, float64(0), record["durationSeconds"])
}

func TestVeoSubmissionErrorIsRecordedOnce(t *testing.T) {
	c := newStubbedClient(t, ProviderGoogleAI, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusBadRequest, `{"error":{"code":400,"message":"invalid durationSeconds","status":"INVALID_ARGUMENT"}}`)
	})

	_, err := c.client.Models().GenerateVideo(context.Background(), geminiVeoModel, "a cat surfing", nil, secondsOf(3))
	require.Error(t, err)

	record := singleRecord(t, c)
	assert.Equal(t, "ERROR", record["stopReason"])
	assert.Equal(t, "INVALID_ARGUMENT (HTTP 400)", record["errorReason"])
	assert.Equal(t, float64(400), record["errorCode"])
	assert.Equal(t, float64(0), record["durationSeconds"])
}

func TestVeoWaitAbandonedBeforeCompletionIsRecordedWhenPolledLater(t *testing.T) {
	c := newStubbedClient(t, ProviderGoogleAI, geminiVeo(1).handle)

	op, err := c.client.Models().GenerateVideo(context.Background(), geminiVeoModel, "a cat surfing", nil, nil)
	require.NoError(t, err)
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = c.client.Models().WaitForVideo(cancelled, geminiVeoModel, op, "generation", nil)
	require.ErrorIs(t, err, context.Canceled)
	pollLikeGoogleDocs(t, c, op)

	assertVideoRecord(t, singleRecord(t, c), geminiVeoModel, veoDefaultClipSeconds)
}

func TestVeoWaitForVideoReportsWaitCallMetadataAndSubtype(t *testing.T) {
	fastVideoPolling(t)
	c := newStubbedClient(t, ProviderGoogleAI, geminiVeo(1).handle)
	submitCtx := core.WithUsageMetadata(context.Background(), map[string]interface{}{"productName": "studio", "traceId": "submit-trace"})
	waitCtx := core.WithUsageMetadata(context.Background(), map[string]interface{}{"traceId": "wait-trace"})

	op, err := c.client.Models().GenerateVideo(submitCtx, geminiVeoModel, "a cat surfing", nil, secondsOf(6))
	require.NoError(t, err)
	_, err = c.client.Models().WaitForVideo(waitCtx, geminiVeoModel, op, "extend", secondsOf(4))
	require.NoError(t, err)

	record := singleRecord(t, c)
	assertVideoRecord(t, record, geminiVeoModel, 6)
	assert.Equal(t, "wait-trace", record["traceId"])
	assert.Equal(t, "studio", record["productName"])
	assert.Equal(t, "extend", record["operationSubtype"])
}

type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (f *fakeClock) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}

func (f *fakeClock) advance(d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.now = f.now.Add(d)
}

func useFakeClock(c *stubbedClient) *fakeClock {
	clock := &fakeClock{now: time.Now()}
	c.client.videos.now = clock.Now
	return clock
}

func TestVeoSubmissionPolledAWeekLaterIsRecordedOnce(t *testing.T) {
	fastVideoPolling(t)
	c := newStubbedClient(t, ProviderGoogleAI, geminiVeo(1).handle)
	clock := useFakeClock(c)
	submitCtx := core.WithUsageMetadata(context.Background(), map[string]interface{}{"traceId": "submit-trace"})

	op, err := c.client.Models().GenerateVideo(submitCtx, geminiVeoModel, "a cat surfing", nil, secondsOf(6))
	require.NoError(t, err)
	clock.advance(7 * 24 * time.Hour)
	c.client.videos.track("models/veo-3.1-generate-preview/operations/submitted-meanwhile", videoRequest{})
	_, err = c.client.Models().WaitForVideo(context.Background(), geminiVeoModel, op, "generation", nil)
	require.NoError(t, err)

	record := singleRecord(t, c)
	assertVideoRecord(t, record, geminiVeoModel, 6)
	assert.Equal(t, "submit-trace", record["traceId"])
}

func TestVeoFinishedOperationSeenAgainKeepsItsIdempotencyKey(t *testing.T) {
	var sent []*metering.MeteringPayload
	tracker := newVideoTracker(ProviderGoogleAI, func(p *metering.MeteringPayload) { sent = append(sent, p) })
	finished := &genai.GenerateVideosOperation{Name: geminiVeoOperation, Done: true, Response: &genai.GenerateVideosResponse{GeneratedVideos: []*genai.GeneratedVideo{{}}}}
	seen := func() {
		tracker.trackWait(finished.Name, videoRequest{model: geminiVeoModel})
		tracker.settleOperation(finished)
	}

	seen()
	seen()
	require.Len(t, sent, 1, "a finished operation still remembered sends no second record")

	for i := 0; i < settledOperationLimit; i++ {
		name := fmt.Sprintf("models/veo/operations/%d", i)
		tracker.track(name, videoRequest{})
		tracker.settle(name, videoResult{videoCount: 1})
	}
	seen()

	var replays []*metering.MeteringPayload
	for _, payload := range sent {
		if payload.TransactionID == sent[0].TransactionID {
			replays = append(replays, payload)
		}
	}
	require.Len(t, replays, 2, "once forgotten, the operation is sent again under its own key")
	assert.Equal(t, replays[0].IdempotencyKey, replays[1].IdempotencyKey)
	assert.Equal(t, operationTransactionID(geminiVeoOperation), replays[1].IdempotencyKey)
}

func TestVeoRecordsOfSameOperationShareIdempotencyKey(t *testing.T) {
	stub := geminiVeo(1)
	stub.submit = stub.done
	first := newStubbedClient(t, ProviderGoogleAI, stub.handle)
	second := newStubbedClient(t, ProviderGoogleAI, stub.handle)

	op, err := first.client.Models().GenerateVideo(context.Background(), geminiVeoModel, "a cat surfing", nil, nil)
	require.NoError(t, err)
	_, err = second.client.Models().WaitForVideo(context.Background(), geminiVeoModel, op, "generation", nil)
	require.NoError(t, err)

	firstRecord, secondRecord := singleRecord(t, first), singleRecord(t, second)
	assert.Equal(t, firstRecord["transactionId"], secondRecord["transactionId"])
	firstKey := first.revenium.GetRequests()[0].Headers.Get("Idempotency-Key")
	assert.NotEmpty(t, firstKey)
	assert.Equal(t, firstKey, second.revenium.GetRequests()[0].Headers.Get("Idempotency-Key"))
}

func TestVeoSettledOperationsAreBoundedBySize(t *testing.T) {
	tracker := newVideoTracker(ProviderGoogleAI, func(*metering.MeteringPayload) {})
	for i := 0; i <= settledOperationLimit; i++ {
		name := fmt.Sprintf("models/veo/operations/%d", i)
		tracker.track(name, videoRequest{})
		tracker.settle(name, videoResult{videoCount: 1})
	}

	assert.Len(t, tracker.settled, settledOperationLimit)
	assert.NotContains(t, tracker.settled, "models/veo/operations/0")
	assert.Contains(t, tracker.settled, fmt.Sprintf("models/veo/operations/%d", settledOperationLimit))
}

func TestVeoPendingOperationsAreBoundedBySize(t *testing.T) {
	tracker := newVideoTracker(ProviderGoogleAI, func(*metering.MeteringPayload) {})
	for i := 0; i <= pendingOperationLimit; i++ {
		tracker.track(fmt.Sprintf("models/veo/operations/%d", i), videoRequest{})
	}

	assert.Len(t, tracker.pending, pendingOperationLimit)
	assert.NotContains(t, tracker.pending, "models/veo/operations/0")
	assert.Contains(t, tracker.pending, fmt.Sprintf("models/veo/operations/%d", pendingOperationLimit))
}

func TestVeoPendingOrderStaysBoundedAsVideosFinish(t *testing.T) {
	tracker := newVideoTracker(ProviderGoogleAI, func(*metering.MeteringPayload) {})
	tracker.track("models/veo/operations/still-rendering", videoRequest{})
	for i := 0; i < 3*pendingOperationLimit; i++ {
		name := fmt.Sprintf("models/veo/operations/%d", i)
		tracker.track(name, videoRequest{})
		tracker.settle(name, videoResult{videoCount: 1})
	}

	assert.LessOrEqual(t, len(tracker.pendingOrder), 2*pendingOperationLimit)
	assert.Contains(t, tracker.pending, "models/veo/operations/still-rendering")
}
