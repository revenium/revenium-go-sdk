package fal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/revenium/revenium-go-sdk/core/metering"
	"github.com/revenium/revenium-go-sdk/core/prompt"
)

const defaultQueueBaseURL = "https://queue.fal.run"

const (
	backgroundRetryPause    = time.Second
	maxBackgroundRetryPause = 30 * time.Second
)

var errQueueJobFailed = errors.New("fal reported the queued request failed")

// Subscribe enqueues a request, polls until completion, and returns the final result.
func (r *ReveniumFal) Subscribe(ctx context.Context, endpointID string, input map[string]interface{}, metadata map[string]interface{}) (map[string]interface{}, error) {
	mergedMeta := core.UsageMetadataOver(ctx, metadata)
	if err := r.enforcer.Check(evalContext(ctx, mergedMeta, endpointID)); err != nil {
		core.Debug("Subscribe blocked by enforcement: %v", err)
		return nil, err
	}

	startTime := time.Now()

	queueBase := r.queueBaseURL()
	body, err := json.Marshal(input)
	if err != nil {
		return nil, core.NewProviderError("failed to marshal input", err)
	}

	submit, err := r.submitQueued(ctx, endpointURL(queueBase, endpointID), body)
	if err != nil {
		r.recordSubmitFailure(endpointID, mergedMeta, startTime, err)
		return nil, err
	}

	job := queuedJob{
		endpointID: endpointID,
		requestID:  submit.RequestID,
		queueBase:  queueBase,
		input:      meteredInputFields(input),
		metadata:   core.CloneMetadata(mergedMeta),
		startTime:  startTime,
	}
	result, err := r.awaitQueuedResult(ctx, job)
	if errors.Is(err, errQueueJobFailed) {
		r.recordFailure(job.endpointID, job.metadata, job.startTime, false, err)
		return nil, err
	}
	if err != nil {
		r.settleUnresolvedJob(job, err)
		return nil, err
	}

	r.meterResult(endpointID, result, job.metadata, time.Since(startTime), startTime, job.input)
	return result, nil
}

// recordSubmitFailure records a queued request fal refused with a 4xx status. A submit that got
// no answer, a 5xx answer, or an answer the SDK could not read may still have queued a job that
// fal runs and bills, so it is not recorded as failed; the warning says its usage may be missing.
// Runway task creation follows the same rule.
func (r *ReveniumFal) recordSubmitFailure(endpointID string, metadata map[string]interface{}, startTime time.Time, err error) {
	if !core.IsClientRefusal(err) {
		core.Warn("fal request to %s was not confirmed as queued (%s); if fal queued it, its usage is not recorded", endpointID, prompt.RedactedError(err))
		return
	}
	r.recordFailure(endpointID, metadata, startTime, false, err)
}

func (r *ReveniumFal) submitQueued(ctx context.Context, submitURL string, body []byte) (*QueueSubmitResponse, error) {
	submitBody, _, err := r.falClient.doRequest(ctx, "POST", submitURL, body, nil)
	if err != nil {
		return nil, err
	}

	var submit QueueSubmitResponse
	if err := json.Unmarshal(submitBody, &submit); err != nil {
		return nil, core.NewProviderError("failed to parse queue submit response", err)
	}
	if submit.RequestID == "" {
		return nil, core.NewProviderError("queue submit response missing request_id", nil)
	}
	return &submit, nil
}

type queuedJob struct {
	endpointID string
	requestID  string
	queueBase  string
	input      map[string]interface{}
	metadata   map[string]interface{}
	startTime  time.Time
}

func (r *ReveniumFal) awaitQueuedResult(ctx context.Context, job queuedJob) (map[string]interface{}, error) {
	if err := r.pollQueueStatus(ctx, job.queueBase, job.endpointID, job.requestID); err != nil {
		return nil, err
	}

	resultURL := fmt.Sprintf("%s/requests/%s", endpointURL(job.queueBase, job.endpointID), job.requestID)
	resultBody, _, err := r.falClient.doRequest(ctx, "GET", resultURL, nil, nil)
	if err != nil {
		return nil, err
	}

	result := map[string]interface{}{}
	if len(resultBody) > 0 {
		if jsonErr := json.Unmarshal(resultBody, &result); jsonErr != nil {
			return nil, core.NewProviderError("failed to parse queue result", jsonErr)
		}
	}
	return result, nil
}

// settleUnresolvedJob keeps polling a queued job whose outcome the caller did not get, because
// fal still runs and bills it, and records the outcome fal reports, a failed job included.
func (r *ReveniumFal) settleUnresolvedJob(job queuedJob, err error) {
	core.Info("Outcome of fal request %s not received (%s); polling it in the background so its usage is recorded", job.requestID, prompt.RedactedError(err))
	r.abandoned.Start(func(stop context.Context) {
		waitCtx, cancel := context.WithTimeout(stop, r.abandonedJobWaitLimit())
		defer cancel()
		result, err := r.retryQueuedResult(waitCtx, job)
		switch {
		case err == nil:
			r.meterResult(job.endpointID, result, job.metadata, time.Since(job.startTime), job.startTime, job.input)
		case errors.Is(err, errQueueJobFailed):
			r.recordFailure(job.endpointID, job.metadata, job.startTime, false, err)
		default:
			r.meterUnfinishedJob(job)
		}
	})
}

func (r *ReveniumFal) retryQueuedResult(ctx context.Context, job queuedJob) (map[string]interface{}, error) {
	pause := backgroundRetryPause
	for {
		result, err := r.awaitQueuedResult(ctx, job)
		if err == nil || errors.Is(err, errQueueJobFailed) || ctx.Err() != nil {
			return result, err
		}
		core.Debug("Background poll of fal request %s failed, retrying in %v: %v", job.requestID, pause, err)
		select {
		case <-ctx.Done():
			return nil, err
		case <-time.After(pause):
		}
		pause = min(2*pause, maxBackgroundRetryPause)
	}
}

// maxAbandonedWait is core.AbandonedWaitLimit, held in a variable so tests can shorten it.
var maxAbandonedWait = core.AbandonedWaitLimit

func (r *ReveniumFal) abandonedJobWaitLimit() time.Duration {
	limit := defaultRequestTimeout
	if r.config != nil && r.config.RequestTimeout > 0 {
		limit = r.config.RequestTimeout
	}
	return min(limit, maxAbandonedWait)
}

func (r *ReveniumFal) meterUnfinishedJob(job queuedJob) {
	if !r.enabled.Load() {
		return
	}
	reason := fmt.Sprintf("fal request %s had not finished when the SDK stopped waiting for it; its outcome is unknown", job.requestID)
	core.Warn("%s. Recording it with the requested quantities", reason)
	op := r.operationOf(job.endpointID)
	payload := buildPayloadFromResult(job.endpointID, op, nil, job.input, job.metadata, time.Since(job.startTime), job.startTime, prompt.CapturesFor(job.metadata, r.config.CapturePrompts), promptFromInput(job.input))
	if op == metering.OperationImage {
		requested := requestedImageCount(job.input)
		payload.ActualImageCount = &requested
		payload.RequestedImageCount = &requested
	}
	payload.TransactionID = job.requestID
	payload.ErrorReason = reason
	payload.StopReason = string(core.StopReasonError)
	r.metering.Send(payload)
}

func (r *ReveniumFal) pollQueueStatus(ctx context.Context, queueBase, endpointID, requestID string) error {
	statusURL := fmt.Sprintf("%s/requests/%s/status", endpointURL(queueBase, endpointID), requestID)
	backoff := 500 * time.Millisecond
	const maxBackoff = 5 * time.Second

	for {
		select {
		case <-ctx.Done():
			return core.NewNetworkError("queue polling cancelled", ctx.Err())
		default:
		}

		body, _, err := r.falClient.doRequest(ctx, "GET", statusURL, nil, nil)
		if err != nil {
			return err
		}

		var status QueueStatusResponse
		if err := json.Unmarshal(body, &status); err != nil {
			return core.NewProviderError("failed to parse queue status", err)
		}

		switch status.Status {
		case QueueCompleted:
			return nil
		case QueueFailed, QueueError:
			return core.NewProviderError(fmt.Sprintf("queue request %s ended with status %s", requestID, status.Status), errQueueJobFailed).
				WithDetails(core.DetailProviderErrorKind, "queue_request_"+strings.ToLower(string(status.Status)))
		}

		select {
		case <-ctx.Done():
			return core.NewNetworkError("queue polling cancelled", ctx.Err())
		case <-time.After(backoff):
		}

		backoff *= 2
		if backoff > maxBackoff {
			backoff = maxBackoff
		}
	}
}

func (r *ReveniumFal) queueBaseURL() string {
	if r.config != nil && r.config.QueueBaseURL != "" {
		return r.config.QueueBaseURL
	}
	return defaultQueueBaseURL
}
