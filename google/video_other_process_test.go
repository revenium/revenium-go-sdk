package google

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/revenium/revenium-go-sdk/core/metering"
	"github.com/revenium/revenium-go-sdk/core/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newPollerAgainst(t *testing.T, provider Provider, submitter *stubbedClient) *stubbedClient {
	t.Helper()
	revenium := testutil.NewMockMeteringServer()
	t.Cleanup(revenium.Close)
	client := newClientAgainst(t, provider, submitter.provider.server.URL, &core.ReveniumConfig{APIKey: "hak_test", BaseURL: revenium.URL()})
	return &stubbedClient{client: client, provider: submitter.provider, revenium: revenium}
}

func TestVeoPolledByAnotherClientIsRecordedOnceByThePoller(t *testing.T) {
	cases := []struct {
		provider    Provider
		model       string
		recordModel string
		operation   string
		stub        *veoStub
	}{
		{ProviderGoogleAI, geminiVeoModel, geminiVeoModel, geminiVeoOperation, geminiVeo(2)},
		{ProviderVertexAI, vertexVeoModel, "vertex_ai/" + vertexVeoModel, vertexVeoOperation, vertexVeo()},
	}
	for _, tc := range cases {
		t.Run(tc.provider.String(), func(t *testing.T) {
			submitter := newStubbedClient(t, tc.provider, tc.stub.handle)
			op, err := submitter.client.Models().GenerateVideo(context.Background(), tc.model, "a cat surfing", nil, nil)
			require.NoError(t, err)

			poller := newPollerAgainst(t, tc.provider, submitter)
			pollCtx := core.WithUsageMetadata(context.Background(), map[string]interface{}{"traceId": "worker-trace"})
			for !op.Done {
				op, err = poller.client.GetGenaiClient().Operations.GetVideosOperation(pollCtx, op, nil)
				require.NoError(t, err)
			}
			_, err = poller.client.GetGenaiClient().Operations.GetVideosOperation(pollCtx, op, nil)
			require.NoError(t, err)

			videos := float64(len(op.Response.GeneratedVideos))
			record := singleRecord(t, poller)
			assertVideoRecord(t, record, tc.recordModel, veoDefaultClipSeconds*videos)
			assert.Equal(t, "worker-trace", record["traceId"])
			assert.Equal(t, operationTransactionID(tc.operation), record["transactionId"])
			assert.Empty(t, submitter.records(t), "the submitting client never saw the video finish")
		})
	}
}

func TestVeoRecordedByTheSubmitterAndAPollerSharesItsIdsWhateverEitherCallerSet(t *testing.T) {
	callerIDs := map[string]interface{}{"transactionId": "order-42", "idempotencyKey": "veo-job-7"}
	cases := []struct {
		name              string
		submitterMetadata map[string]interface{}
		pollerMetadata    map[string]interface{}
		pollerFirst       bool
	}{
		{"submitter sets ids and records first", callerIDs, nil, false},
		{"poller sets ids and records first", nil, callerIDs, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fastVideoPolling(t)
			submitter := newStubbedClient(t, ProviderGoogleAI, geminiVeo(1).handle)
			submitCtx := core.WithUsageMetadata(context.Background(), tc.submitterMetadata)
			op, err := submitter.client.Models().GenerateVideo(submitCtx, geminiVeoModel, "a cat surfing", nil, secondsOf(4))
			require.NoError(t, err)
			poller := newPollerAgainst(t, ProviderGoogleAI, submitter)
			pollCtx := core.WithUsageMetadata(context.Background(), tc.pollerMetadata)
			waitInSubmitter := func() {
				_, err := submitter.client.Models().WaitForVideo(submitCtx, geminiVeoModel, op, "", nil)
				require.NoError(t, err)
			}
			pollInPoller := func() {
				polled := op
				for !polled.Done {
					polled, err = poller.client.GetGenaiClient().Operations.GetVideosOperation(pollCtx, polled, nil)
					require.NoError(t, err)
				}
			}

			if tc.pollerFirst {
				pollInPoller()
				waitInSubmitter()
			} else {
				waitInSubmitter()
				pollInPoller()
			}

			operationID := operationTransactionID(geminiVeoOperation)
			for _, side := range []*stubbedClient{submitter, poller} {
				assert.Equal(t, operationID, singleRecord(t, side)["transactionId"])
				assert.Equal(t, operationID, side.revenium.GetRequests()[0].Headers.Get("Idempotency-Key"))
			}
			withCallerIDs := submitter
			if tc.pollerMetadata != nil {
				withCallerIDs = poller
			}
			assert.Equal(t, "order-42", singleRecord(t, withCallerIDs)["traceId"], "the caller's transactionId is kept as the trace id")
		})
	}
}

func TestVeoFailurePolledByAnotherClientIsRecordedOnceAsError(t *testing.T) {
	submitter := newStubbedClient(t, ProviderGoogleAI, failedVeo().handle)
	op, err := submitter.client.Models().GenerateVideo(context.Background(), geminiVeoModel, "a cat surfing", nil, nil)
	require.NoError(t, err)

	poller := newPollerAgainst(t, ProviderGoogleAI, submitter)
	for !op.Done {
		op, err = poller.client.GetGenaiClient().Operations.GetVideosOperation(context.Background(), op, nil)
		require.NoError(t, err)
	}

	record := singleRecord(t, poller)
	assert.Equal(t, geminiVeoModel, record["model"])
	assert.Equal(t, "ERROR", record["stopReason"])
	assert.Equal(t, 0.0, record["durationSeconds"])
}

func TestFinishedOperationThatIsNotVeoIsNotRecorded(t *testing.T) {
	var sent []*metering.MeteringPayload
	tracker := newVideoTracker(ProviderGoogleAI, func(p *metering.MeteringPayload) { sent = append(sent, p) })

	for _, name := range []string{
		"tunedModels/my-model/operations/op1",
		"fileSearchStores/store/operations/op1",
		"models/gemini-3.5-flash/operations/op1",
		"operations/op1",
	} {
		tracker.observe(context.Background(), []byte(`{"name":"`+name+`","done":true,"response":{}}`))
	}

	assert.Empty(t, sent)
}

type watchedBody struct {
	io.Reader
	read bool
}

func (b *watchedBody) Read(p []byte) (int, error) {
	b.read = true
	return b.Reader.Read(p)
}

func (b *watchedBody) Close() error { return nil }

type cannedResponse struct{ body *watchedBody }

func (c cannedResponse) RoundTrip(*http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: http.StatusOK, Body: c.body}, nil
}

func pollThroughTracker(t *testing.T, method, path string) (bodyRead bool, records int) {
	t.Helper()
	var sent []*metering.MeteringPayload
	tracker := newVideoTracker(ProviderGoogleAI, func(p *metering.MeteringPayload) { sent = append(sent, p) })
	body := &watchedBody{Reader: strings.NewReader(`{"name":"` + geminiVeoOperation + `","done":true,"response":{}}`)}
	transport := &videoOperationTransport{base: cannedResponse{body: body}, tracker: tracker}
	req, err := http.NewRequest(method, "https://generativelanguage.googleapis.com"+path, nil)
	require.NoError(t, err)

	resp, err := transport.RoundTrip(req)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	return body.read, len(sent)
}

func TestOperationPollsThatAreNotVeoAreNotRead(t *testing.T) {
	for _, poll := range []struct{ method, path string }{
		{http.MethodGet, "/v1beta/tunedModels/my-model/operations/op1"},
		{http.MethodGet, "/v1beta1/projects/p1/locations/us-central1/tuningJobs/123/operations/op1"},
		{http.MethodGet, "/v1beta/batches/123/operations/op1"},
		{http.MethodGet, "/v1beta/models/gemini-3.5-flash/operations/op1"},
		{http.MethodPost, "/v1beta1/projects/p1/locations/us-central1/publishers/google/models/imagen-4.0-generate-001:fetchPredictOperation"},
	} {
		read, records := pollThroughTracker(t, poll.method, poll.path)
		assert.False(t, read, poll.path)
		assert.Zero(t, records, poll.path)
	}
}

func TestVeoOperationPollsAreRead(t *testing.T) {
	for _, poll := range []struct{ method, path string }{
		{http.MethodGet, "/v1beta/" + geminiVeoOperation},
		{http.MethodPost, "/v1beta1/projects/p1/locations/us-central1/publishers/google/models/" + vertexVeoModel + ":fetchPredictOperation"},
	} {
		read, records := pollThroughTracker(t, poll.method, poll.path)
		assert.True(t, read, poll.path)
		assert.Equal(t, 1, records, poll.path)
	}
}

func TestVeoModelOfOperation(t *testing.T) {
	cases := map[string]string{
		geminiVeoOperation: geminiVeoModel,
		vertexVeoOperation: vertexVeoModel,
	}
	for name, want := range cases {
		model, ok := veoModelOfOperation(name)
		assert.True(t, ok, name)
		assert.Equal(t, want, model)
	}
	_, ok := veoModelOfOperation("models/imagen-4.0-generate-001/operations/op1")
	assert.False(t, ok)
}

func TestVeoSubmittedAndPolledByOneClientKeepsTheRequestedLength(t *testing.T) {
	c := newStubbedClient(t, ProviderGoogleAI, geminiVeo(1).handle)

	op, err := c.client.Models().GenerateVideo(context.Background(), geminiVeoModel, "a cat surfing", nil, secondsOf(4))
	require.NoError(t, err)
	pollLikeGoogleDocs(t, c, op)

	assertVideoRecord(t, singleRecord(t, c), geminiVeoModel, 4)
}

func TestReadmeSaysWhichProcessRecordsAVeoVideo(t *testing.T) {
	readme, err := os.ReadFile(filepath.Join("..", "README.md"))
	require.NoError(t, err)
	text := strings.Join(strings.Fields(string(readme)), " ")

	assert.Contains(t, text, "the polling process records it")
	assert.Contains(t, text, "is recorded at 8 seconds per video; to record the requested length there, wait with `WaitForVideo()`")
}
