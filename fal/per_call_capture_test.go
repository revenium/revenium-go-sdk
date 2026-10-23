package fal

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type captureDirection struct {
	clientCapture bool
	callCapture   bool
}

var captureDirections = map[string]captureDirection{
	"call turns capture off on a capture-on client": {clientCapture: true, callCapture: false},
	"call turns capture on on a capture-off client": {clientCapture: false, callCapture: true},
}

func assertCaptureFollowsTheCall(t *testing.T, record map[string]interface{}, callCapture bool) {
	t.Helper()
	if !callCapture {
		assert.NotContains(t, record, "inputMessages")
		assert.NotContains(t, record, "outputResponse")
		return
	}
	prompt := capturedPromptText(t, record)
	assert.NotContains(t, prompt, "FALSECRET", "a prompt captured on the call's request is still redacted")
	assert.Contains(t, prompt, "a lighthouse")
	assert.Contains(t, record, "outputResponse")
}

const capturedCallPrompt = "api_key=" + leakedKey + " a lighthouse"

func TestRun_PerCallCaptureSettingBeatsTheClients(t *testing.T) {
	for name, direction := range captureDirections {
		t.Run(name, func(t *testing.T) {
			falServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(videoFileOutput))
			}))
			t.Cleanup(falServer.Close)
			client, mock := newFalAgainst(t, falServer.URL)
			client.config.CapturePrompts = direction.clientCapture

			_, err := client.Run(context.Background(), slowVideoEndpoint, map[string]interface{}{"prompt": capturedCallPrompt},
				map[string]interface{}{"capturePrompts": direction.callCapture})
			require.NoError(t, err)

			require.True(t, mock.WaitForPayloads(1, 2*time.Second))
			assertCaptureFollowsTheCall(t, mock.GetPayloads()[0], direction.callCapture)
		})
	}
}

func TestSubscribe_PerCallCaptureSettingBeatsTheClients(t *testing.T) {
	for name, direction := range captureDirections {
		t.Run(name, func(t *testing.T) {
			queue := newSlowQueue(t)
			queue.finished.Store(true)
			client, mock := newFalAgainst(t, queue.server.URL)
			client.config.CapturePrompts = direction.clientCapture

			_, err := client.Subscribe(context.Background(), slowVideoEndpoint, map[string]interface{}{"prompt": capturedCallPrompt},
				map[string]interface{}{"capturePrompts": direction.callCapture})
			require.NoError(t, err)

			require.True(t, mock.WaitForPayloads(1, 2*time.Second))
			assertCaptureFollowsTheCall(t, mock.GetPayloads()[0], direction.callCapture)
		})
	}
}

func TestSubscribe_UnfinishedJobFollowsThePerCallCaptureSetting(t *testing.T) {
	for name, direction := range captureDirections {
		t.Run(name, func(t *testing.T) {
			queue := newSlowQueue(t)
			client, mock := newFalAgainst(t, queue.server.URL)
			client.config.CapturePrompts = direction.clientCapture

			short, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()
			_, err := client.Subscribe(short, slowVideoEndpoint, map[string]interface{}{"prompt": capturedCallPrompt},
				map[string]interface{}{"capturePrompts": direction.callCapture})
			require.ErrorIs(t, err, context.DeadlineExceeded)
			require.NoError(t, client.Close())

			payloads := mock.GetPayloads()
			require.Len(t, payloads, 1)
			record := payloads[0]
			if direction.callCapture {
				assert.Contains(t, capturedPromptText(t, record), "a lighthouse")
				return
			}
			assert.NotContains(t, record, "inputMessages")
		})
	}
}
