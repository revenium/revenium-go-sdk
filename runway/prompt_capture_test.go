package runway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func capturedVideoRecord(t *testing.T, promptText string, outputs []string) map[string]interface{} {
	t.Helper()
	outputJSON, err := json.Marshal(outputs)
	require.NoError(t, err)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasPrefix(r.URL.Path, "/v1/tasks/") {
			_, _ = w.Write([]byte(`{"id":"task-c","status":"SUCCEEDED","output":` + string(outputJSON) + `}`))
			return
		}
		_, _ = w.Write([]byte(`{"id":"task-c","status":"PENDING"}`))
	}))
	t.Cleanup(server.Close)
	client, mock := newRunwayAgainst(t, server.URL)
	client.config.CapturePrompts = true

	_, err = client.ImageToVideo(context.Background(), &ImageToVideoRequest{PromptImage: "https://x/i.png", PromptText: promptText}, nil)
	require.NoError(t, err)
	client.Flush()
	require.True(t, mock.WaitForPayloads(1, 2*time.Second))
	return mock.GetPayloads()[0]
}

func capturedPromptText(t *testing.T, record map[string]interface{}) string {
	t.Helper()
	raw, ok := record["inputMessages"].(string)
	require.True(t, ok, "inputMessages missing")
	var messages []map[string]string
	require.NoError(t, json.Unmarshal([]byte(raw), &messages))
	require.Len(t, messages, 1)
	return messages[0]["content"]
}

func TestImageToVideo_CapturedPromptHasCredentialsRedacted(t *testing.T) {
	record := capturedVideoRecord(t, "use api_key=sk-live-RUNWAYSECRET1234567890abcdef for a sunset", []string{"https://x/out.mp4"})

	prompt := capturedPromptText(t, record)
	assert.NotContains(t, prompt, "RUNWAYSECRET")
	assert.Contains(t, prompt, "for a sunset")
	assert.Equal(t, `["https://x/out.mp4"]`, record["outputResponse"])
}

func TestImageToVideo_CapturedPromptAndAnswerHonourTheSizeLimit(t *testing.T) {
	t.Setenv(core.EnvMaxPromptSize, "1000")
	outputs := make([]string, 40)
	for i := range outputs {
		outputs[i] = "https://x/" + strings.Repeat("o", 60) + ".mp4"
	}

	record := capturedVideoRecord(t, strings.Repeat("p", 3095), outputs)

	assert.LessOrEqual(t, utf8.RuneCountInString(capturedPromptText(t, record)), 1000)
	assert.LessOrEqual(t, utf8.RuneCountInString(record["outputResponse"].(string)), 1000)
	assert.Equal(t, true, record["promptsTruncated"])
}
