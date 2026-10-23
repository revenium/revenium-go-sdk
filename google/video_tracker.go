package google

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/revenium/revenium-go-sdk/core/metering"
	"google.golang.org/genai"
)

// Veo renders 8-second clips when durationSeconds is omitted, and Google reports no
// clip length on the finished operation: https://ai.google.dev/gemini-api/docs/video
const veoDefaultClipSeconds = 8

// Submitted videos still waiting to be seen finished. They never expire by time, because
// a caller may resume polling days later; only a client holding this many unread results
// drops the oldest.
const pendingOperationLimit = 10000

// Finished operation names remembered so a later sighting sends no second record; at
// roughly a hundred bytes per name this is about a megabyte.
const settledOperationLimit = 10000

type videoRequest struct {
	model           string
	subtype         string
	clipSeconds     float64
	requestedVideos int
	resolution      string
	metadata        map[string]interface{}
	requestTime     time.Time
}

func newVideoRequest(ctx context.Context, model, subtype string, config *genai.GenerateVideosConfig) videoRequest {
	request := videoRequest{
		model:           model,
		subtype:         subtype,
		clipSeconds:     veoDefaultClipSeconds,
		requestedVideos: 1,
		metadata:        core.GetUsageMetadata(ctx),
		requestTime:     time.Now(),
	}
	if config == nil {
		return request
	}
	if config.DurationSeconds != nil && *config.DurationSeconds > 0 {
		request.clipSeconds = float64(*config.DurationSeconds)
	}
	if config.NumberOfVideos > 0 {
		request.requestedVideos = int(config.NumberOfVideos)
	}
	request.resolution = config.Resolution
	return request
}

type videoResult struct {
	videoCount int
	failure    *metering.CallFailure
}

func videoFailure(operationError map[string]any) *metering.CallFailure {
	status, _ := operationError["status"].(string)
	kind := core.ErrorKindFrom(status)
	if kind == "" {
		kind = "video_generation_failed"
	}
	return &metering.CallFailure{
		Err:           fmt.Errorf("video generation failed: %v", operationError),
		Kind:          kind,
		ProviderReply: true,
	}
}

func resultOfOperation(op *genai.GenerateVideosOperation) videoResult {
	if op.Error != nil {
		return videoResult{failure: videoFailure(op.Error)}
	}
	if op.Response == nil {
		return videoResult{}
	}
	return videoResult{videoCount: len(op.Response.GeneratedVideos)}
}

type operationStatus struct {
	Name     string         `json:"name"`
	Done     bool           `json:"done"`
	Error    map[string]any `json:"error"`
	Response struct {
		GenerateVideoResponse struct {
			GeneratedSamples []json.RawMessage `json:"generatedSamples"`
		} `json:"generateVideoResponse"`
		Videos []json.RawMessage `json:"videos"`
	} `json:"response"`
}

func (s operationStatus) result() videoResult {
	if s.Error != nil {
		return videoResult{failure: videoFailure(s.Error)}
	}
	return videoResult{videoCount: len(s.Response.GenerateVideoResponse.GeneratedSamples) + len(s.Response.Videos)}
}

// videoTracker sends exactly one record per Veo operation, at the first moment its
// outcome is seen, whichever client call observes it.
type videoTracker struct {
	provider Provider
	send     func(*metering.MeteringPayload)
	now      func() time.Time

	mu           sync.Mutex
	pending      map[string]*videoRequest
	pendingOrder []string
	settled      map[string]struct{}
	settledOrder []string
}

func newVideoTracker(provider Provider, send func(*metering.MeteringPayload)) *videoTracker {
	return &videoTracker{
		provider: provider,
		send:     send,
		now:      time.Now,
		pending:  map[string]*videoRequest{},
		settled:  map[string]struct{}{},
	}
}

func (t *videoTracker) track(operationName string, request videoRequest) {
	if operationName == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.register(operationName, request)
}

func (t *videoTracker) register(operationName string, request videoRequest) {
	if _, done := t.settled[operationName]; done {
		return
	}
	if _, known := t.pending[operationName]; known {
		return
	}
	t.pending[operationName] = &request
	t.pendingOrder = append(t.pendingOrder, operationName)
	t.evictOldestPending()
}

func (t *videoTracker) evictOldestPending() {
	for len(t.pending) > pendingOperationLimit {
		oldest := t.pendingOrder[0]
		t.pendingOrder = t.pendingOrder[1:]
		if _, waiting := t.pending[oldest]; waiting {
			delete(t.pending, oldest)
			core.Warn("Veo operation %s was never seen finished and is no longer tracked; its video will not be metered", oldest)
		}
	}
	if len(t.pendingOrder) > 2*pendingOperationLimit {
		t.pendingOrder = t.stillPending()
	}
}

func (t *videoTracker) stillPending() []string {
	names := make([]string, 0, len(t.pending))
	for _, name := range t.pendingOrder {
		if _, waiting := t.pending[name]; waiting {
			names = append(names, name)
		}
	}
	return names
}

// trackWait keeps the generation settings recorded at submission and takes the
// reporting fields from the wait call, which is where callers attach them.
func (t *videoTracker) trackWait(operationName string, wait videoRequest) {
	if operationName == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	request, known := t.pending[operationName]
	if !known {
		t.register(operationName, wait)
		return
	}
	request.metadata = core.MergeMetadata(request.metadata, wait.metadata)
	if wait.subtype != "" {
		request.subtype = wait.subtype
	}
}

func (t *videoTracker) rememberSettled(operationName string) {
	t.settled[operationName] = struct{}{}
	t.settledOrder = append(t.settledOrder, operationName)
	if len(t.settledOrder) > settledOperationLimit {
		delete(t.settled, t.settledOrder[0])
		t.settledOrder = t.settledOrder[1:]
	}
}

func (t *videoTracker) settle(operationName string, result videoResult) {
	t.settleWith(operationName, result, nil)
}

// settleWith reports a finished operation once. An operation this tracker did not
// see submitted is reported with the request unknown returns, when it returns one.
func (t *videoTracker) settleWith(operationName string, result videoResult, unknown func() (videoRequest, bool)) {
	t.mu.Lock()
	request, known := t.pending[operationName]
	if !known {
		request = t.unknownOperationRequest(operationName, unknown)
	}
	if request == nil {
		t.mu.Unlock()
		return
	}
	delete(t.pending, operationName)
	t.rememberSettled(operationName)
	t.mu.Unlock()

	t.report(operationName, *request, result)
}

func (t *videoTracker) unknownOperationRequest(operationName string, unknown func() (videoRequest, bool)) *videoRequest {
	if _, done := t.settled[operationName]; done || unknown == nil {
		return nil
	}
	request, ok := unknown()
	if !ok {
		return nil
	}
	return &request
}

func (t *videoTracker) settleOperation(op *genai.GenerateVideosOperation) {
	if op == nil || !op.Done {
		return
	}
	t.settle(op.Name, resultOfOperation(op))
}

// observe settles a finished Veo operation seen in a poll, including one submitted
// by another client or process: Google's operation names the model but not the
// request settings, so that video is recorded at Veo's default clip length.
func (t *videoTracker) observe(ctx context.Context, operationBody []byte) {
	var status operationStatus
	if err := json.Unmarshal(operationBody, &status); err != nil || !status.Done {
		return
	}
	result := status.result()
	t.settleWith(status.Name, result, func() (videoRequest, bool) {
		return requestOfUnknownOperation(ctx, status.Name, result)
	})
}

func requestOfUnknownOperation(ctx context.Context, operationName string, result videoResult) (videoRequest, bool) {
	model, ok := veoModelOfOperation(operationName)
	if !ok {
		return videoRequest{}, false
	}
	request := newVideoRequest(ctx, model, metering.SubtypeGeneration, nil)
	if result.videoCount > 0 {
		request.requestedVideos = result.videoCount
	}
	return request, true
}

// veoModelOfOperation reads the model from a Veo operation name, which ends in
// models/<model>/operations/<id> on the Gemini API and on Vertex AI.
func veoModelOfOperation(operationName string) (string, bool) {
	segments := strings.Split(operationName, "/")
	n := len(segments)
	if n < 4 || segments[n-2] != "operations" {
		return "", false
	}
	return veoModelOfResource(segments[:n-2])
}

func veoModelOfResource(segments []string) (string, bool) {
	n := len(segments)
	if n < 2 || segments[n-2] != "models" || !strings.HasPrefix(segments[n-1], "veo") {
		return "", false
	}
	return segments[n-1], true
}

// operationTransactionID derives one ID per Veo operation so a record sent again for
// the same operation, by another process or after this tracker forgot it, carries the
// same Idempotency-Key and is deduplicated by the platform.
func operationTransactionID(operationName string) string {
	sum := sha256.Sum256([]byte(operationName))
	b := sum[:16]
	b[6] = (b[6] & 0x0f) | 0x50
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func (t *videoTracker) report(operationName string, request videoRequest, result videoResult) {
	provider, model := t.provider.mediaIdentity(metering.OperationVideo, request.model)
	builder := metering.NewPayload(metering.OperationVideo, model, provider).
		WithTiming(request.requestTime, t.now().Sub(request.requestTime)).
		WithOperationSubtype(request.subtype)
	if result.failure != nil {
		payload := builder.WithVideoDuration(0, 0).Build()
		metering.ApplyFailedCall(payload, *result.failure, request.metadata)
		t.send(withOperationIdentity(payload, operationName, request.metadata))
		return
	}
	payload := builder.
		WithVideoDuration(request.clipSeconds*float64(result.videoCount), request.clipSeconds*float64(request.requestedVideos)).
		WithBillingUnit(metering.BillingPerSecond).
		WithResolution(request.resolution).
		Build()
	metering.ApplyMetadata(payload, request.metadata)
	t.send(withOperationIdentity(payload, operationName, request.metadata))
}

// withOperationIdentity gives the record of a Veo operation the transaction id and
// Idempotency-Key derived from the operation name, overriding any transactionId or
// idempotencyKey in the caller's usage metadata: the submitting process and a process
// that only polls carry different metadata, and only matching keys let the platform
// keep one record of the video. As for tool events, a transactionId the caller set
// becomes the record's traceId when it has none; the video endpoint stores no
// usageMetadata, so with a traceId already set it is not sent.
func withOperationIdentity(payload *metering.MeteringPayload, operationName string, metadata map[string]interface{}) *metering.MeteringPayload {
	if operationName == "" {
		return payload
	}
	id := operationTransactionID(operationName)
	payload.TransactionID = id
	payload.IdempotencyKey = id
	if callerID, ok := metadata["transactionId"].(string); ok && !core.IsBlank(callerID) && core.IsBlank(payload.TraceID) {
		payload.TraceID = callerID
	}
	return payload
}

type videoOperationTransport struct {
	base    http.RoundTripper
	tracker *videoTracker
}

// isVeoOperationPoll matches the two requests genai's GetVideosOperation sends: a GET
// of models/<veo model>/operations/<id> on the Gemini API and a POST to
// models/<veo model>:fetchPredictOperation on Vertex AI. Every other operation,
// such as a batch or tuning job, passes through without its body being read.
func isVeoOperationPoll(req *http.Request) bool {
	switch req.Method {
	case http.MethodGet:
		_, ok := veoModelOfOperation(req.URL.Path)
		return ok
	case http.MethodPost:
		resource, found := strings.CutSuffix(req.URL.Path, ":fetchPredictOperation")
		if !found {
			return false
		}
		_, ok := veoModelOfResource(strings.Split(resource, "/"))
		return ok
	default:
		return false
	}
}

func (v *videoOperationTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := v.base.RoundTrip(req)
	if err != nil || resp.StatusCode != http.StatusOK || !isVeoOperationPoll(req) {
		return resp, err
	}
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return nil, err
	}
	resp.Body = io.NopCloser(bytes.NewReader(body))
	v.tracker.observe(req.Context(), body)
	return resp, nil
}
