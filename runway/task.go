package runway

import (
	"context"
	"fmt"
	"time"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/revenium/revenium-go-sdk/core/enforcement"
	"github.com/revenium/revenium-go-sdk/core/prompt"
)

type videoTask struct {
	name   string
	record taskRecord
	create func(ctx context.Context) (*TaskResponse, error)
}

type taskRecord struct {
	model        string
	subtype      string
	videoSeconds float64
	metadata     map[string]interface{}
	resultInfo   map[string]interface{}
}

type acceptedTask struct {
	taskRecord
	id        string
	startTime time.Time
}

func (r *ReveniumRunway) generationResultInfo(videoSeconds float64, promptText string, metadata map[string]interface{}) map[string]interface{} {
	info := map[string]interface{}{"requestedDuration": int(videoSeconds)}
	if prompt.CapturesFor(metadata, r.config.CapturePrompts) && promptText != "" {
		info["_capturedPrompt"] = promptText
	}
	return info
}

func (r *ReveniumRunway) pollingConfig() *PollingConfig {
	if r.polling != nil {
		return r.polling
	}
	return DefaultPollingConfig()
}

func (r *ReveniumRunway) runVideoTask(ctx context.Context, task videoTask) (*VideoGenerationResult, error) {
	startTime := time.Now()

	if err := r.enforcer.Check(enforcement.NewEvalContextFromMetadata(ctx, task.record.metadata, task.record.model, "runway")); err != nil {
		core.Debug("Runway %s blocked by enforcement: %v", task.name, err)
		return nil, err
	}

	core.Debug("Creating %s task with model: %s", task.name, task.record.model)
	taskResp, err := task.create(ctx)
	if err != nil {
		r.recordCreateFailure(task, startTime, err)
		return nil, err
	}
	accepted := acceptedTask{taskRecord: task.record, id: taskResp.ID, startTime: startTime}

	core.Info("Waiting for task %s to complete...", accepted.id)
	statusResp, err := r.runwayClient.WaitForTaskCompletion(ctx, accepted.id, r.pollingConfig())
	if err != nil && endedByRunway(statusResp) {
		r.metering.Send(buildEndedTaskPayload(accepted, statusResp, r.config.CapturePrompts))
		return nil, err
	}
	if err != nil {
		r.settleUnfinishedWait(ctx, accepted)
		return nil, err
	}

	result := accepted.result(statusResp)
	r.sendTaskRecord(accepted, result)
	return result, nil
}

// maxAbandonedWait is core.AbandonedWaitLimit, held in a variable so tests can shorten it.
var maxAbandonedWait = core.AbandonedWaitLimit

// recordCreateFailure records a task Runway refused with a 4xx status. A
// request that got no answer, a 5xx answer, or an answer the SDK could not read, may still
// have created a task that Runway renders and bills, so it is not recorded as
// failed; the warning says its usage may be missing.
func (r *ReveniumRunway) recordCreateFailure(task videoTask, startTime time.Time, err error) {
	if !core.IsClientRefusal(err) {
		core.Warn("Runway %s task was not confirmed (%s); if Runway created it, its usage is not recorded", task.name, prompt.RedactedError(err))
		return
	}
	r.metering.Send(buildRejectedTaskPayload(task.record, startTime, err, r.config.CapturePrompts))
}

// endedByRunway reports whether Runway said the task failed or was canceled.
func endedByRunway(status *TaskStatusResponse) bool {
	return status != nil && (status.Status == TaskStatusFailed || status.Status == TaskStatusCanceled)
}

// settleUnfinishedWait records a task the SDK stopped polling before Runway reported an outcome,
// because Runway keeps rendering and bills it. When the caller's context ended, the task is polled
// in the background and recorded with the outcome Runway reports, failed or canceled included.
func (r *ReveniumRunway) settleUnfinishedWait(ctx context.Context, task acceptedTask) {
	if ctx.Err() == nil {
		r.sendUnfinishedTaskRecord(task)
		return
	}
	core.Info("Caller stopped waiting for Runway task %s; polling it in the background so its usage is recorded", task.id)
	r.abandoned.Start(func(stop context.Context) {
		waitCtx, cancel := context.WithTimeout(stop, r.abandonedTaskWaitLimit())
		defer cancel()
		status, err := r.runwayClient.WaitForTaskCompletion(waitCtx, task.id, r.pollingConfig())
		switch {
		case err == nil:
			r.sendTaskRecord(task, task.result(status))
		case endedByRunway(status):
			r.metering.Send(buildEndedTaskPayload(task, status, r.config.CapturePrompts))
		default:
			r.sendUnfinishedTaskRecord(task)
		}
	})
}

// abandonedTaskWaitLimit bounds the background poll of a task the caller stopped waiting for by the
// client's own polling limit, and by maxAbandonedWait, after which the task is recorded as unfinished.
func (r *ReveniumRunway) abandonedTaskWaitLimit() time.Duration {
	return min(r.pollingConfig().Timeout, maxAbandonedWait)
}

func (t acceptedTask) result(status *TaskStatusResponse) *VideoGenerationResult {
	return &VideoGenerationResult{
		ID:          t.id,
		Status:      status.Status,
		OutputURLs:  status.Output,
		Duration:    time.Since(t.startTime),
		Model:       t.model,
		Error:       status.Error,
		FailureCode: status.FailureCode,
		Metadata:    t.resultInfo,
	}
}

func (r *ReveniumRunway) sendUnfinishedTaskRecord(task acceptedTask) {
	reason := fmt.Sprintf("Runway task %s had not finished when the SDK stopped waiting for it; its outcome is unknown", task.id)
	core.Warn("%s. Recording it with the requested length", reason)
	r.sendTaskRecord(task, &VideoGenerationResult{
		ID:       task.id,
		Status:   TaskStatusRunning,
		Duration: time.Since(task.startTime),
		Model:    task.model,
		Error:    &reason,
		Metadata: task.resultInfo,
	})
}

func (r *ReveniumRunway) sendTaskRecord(task acceptedTask, result *VideoGenerationResult) {
	r.metering.Send(buildVideoMeteringPayload(result, task.subtype, task.videoSeconds, task.metadata, prompt.CapturesFor(task.metadata, r.config.CapturePrompts), task.startTime))
}
