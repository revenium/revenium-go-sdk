package runway

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const capturedCallPrompt = "api_key=sk-live-RUNWAYSECRET1234567890abcdef a lighthouse"

func everyVideoCallWithAPrompt() map[string]videoCall {
	return map[string]videoCall{
		"image to video": func(ctx context.Context, r *ReveniumRunway, metadata *UsageMetadata) error {
			_, err := r.ImageToVideo(ctx, &ImageToVideoRequest{PromptImage: "https://x/i.png", PromptText: capturedCallPrompt}, metadata)
			return err
		},
		"video to video": func(ctx context.Context, r *ReveniumRunway, metadata *UsageMetadata) error {
			_, err := r.VideoToVideo(ctx, &VideoToVideoRequest{PromptVideo: "https://x/v.mp4", PromptText: capturedCallPrompt}, metadata)
			return err
		},
		"upscale": func(ctx context.Context, r *ReveniumRunway, metadata *UsageMetadata) error {
			_, err := r.UpscaleVideo(ctx, &VideoUpscaleRequest{PromptVideo: "https://x/v.mp4", SourceDurationSeconds: 8}, metadata)
			return err
		},
	}
}

func TestVideoRecords_PerCallCaptureSettingBeatsTheClients(t *testing.T) {
	directions := map[string]struct {
		clientCapture bool
		callCapture   bool
	}{
		"call turns capture off on a capture-on client": {clientCapture: true, callCapture: false},
		"call turns capture on on a capture-off client": {clientCapture: false, callCapture: true},
	}
	for callName, call := range everyVideoCallWithAPrompt() {
		for directionName, direction := range directions {
			t.Run(callName+"/"+directionName, func(t *testing.T) {
				client, mock := newStubbedRunway(t)
				client.config.CapturePrompts = direction.clientCapture

				require.NoError(t, call(context.Background(), client, &UsageMetadata{Custom: map[string]interface{}{"capturePrompts": direction.callCapture}}))
				client.Flush()
				require.True(t, mock.WaitForPayloads(1, 2*time.Second))
				record := mock.GetPayloads()[0]

				if !direction.callCapture {
					assert.NotContains(t, record, "inputMessages")
					assert.NotContains(t, record, "outputResponse")
					return
				}
				assert.Equal(t, `["https://x/out.mp4"]`, record["outputResponse"])
				if callName == "upscale" {
					return
				}
				prompt := capturedPromptText(t, record)
				assert.NotContains(t, prompt, "RUNWAYSECRET")
				assert.Contains(t, prompt, "a lighthouse")
			})
		}
	}
}

func TestVideoResult_CarriesThePromptOnlyWhenTheCallCaptures(t *testing.T) {
	client, _ := newStubbedRunway(t)
	client.config.CapturePrompts = true

	result, err := client.ImageToVideo(context.Background(), &ImageToVideoRequest{PromptImage: "https://x/i.png", PromptText: "private"},
		&UsageMetadata{Custom: map[string]interface{}{"capturePrompts": false}})
	require.NoError(t, err)

	assert.NotContains(t, result.Metadata, "_capturedPrompt")
}

func TestFailedCancelledAndRefusedRecords_CarryNoCapturedFieldsWhateverTheSetting(t *testing.T) {
	refuse := func(w http.ResponseWriter) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"bad request"}`))
	}
	outcomes := map[string]func(t *testing.T) string{
		"failed by Runway": func(t *testing.T) string {
			return runwayAnswering(t, acceptTask, `{"id":"task-1","status":"FAILED","failureCode":"SAFETY.INPUT.TEXT"}`)
		},
		"cancelled by Runway": func(t *testing.T) string {
			return runwayAnswering(t, acceptTask, `{"id":"task-1","status":"CANCELED"}`)
		},
		"refused with a 4xx": func(t *testing.T) string { return runwayAnswering(t, refuse, "") },
	}
	settings := map[string]struct{ client, call bool }{
		"both on":             {true, true},
		"call on, client off": {false, true},
		"call off, client on": {true, false},
		"both off":            {false, false},
	}
	for outcomeName, runwayURL := range outcomes {
		for settingName, setting := range settings {
			t.Run(outcomeName+"/"+settingName, func(t *testing.T) {
				client, mock := newRunwayCapturing(t, runwayURL(t), setting.client)

				_, err := client.ImageToVideo(context.Background(), &ImageToVideoRequest{PromptImage: "https://x/i.png", PromptText: capturedCallPrompt},
					&UsageMetadata{Custom: map[string]interface{}{"capturePrompts": setting.call}})
				require.Error(t, err)

				record := onlyRunwayRecord(t, client, mock)
				assert.Equal(t, "FAILED", record["completionStatus"])
				assert.NotContains(t, record, "inputMessages")
				assert.NotContains(t, record, "outputResponse")
			})
		}
	}
}
