package fal

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/revenium/revenium-go-sdk/core/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const leakedKey = "sk-live-FALSECRET1234567890abcdef"

func meterCapturedFalCall(t *testing.T, endpointID, falOutput string, input map[string]interface{}) map[string]interface{} {
	t.Helper()
	falServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(falOutput))
	}))
	defer falServer.Close()
	mock := testutil.NewMockMeteringServer()
	defer mock.Close()

	client, err := NewReveniumFal(&Config{
		FalAPIKey:      "test-key",
		FalBaseURL:     falServer.URL,
		QueueBaseURL:   falServer.URL,
		RequestTimeout: 10 * time.Second,
		CapturePrompts: true,
		Revenium:       &core.ReveniumConfig{APIKey: "hak_test_key_123", BaseURL: mock.URL()},
	})
	require.NoError(t, err)
	defer client.Close()

	_, err = client.Run(context.Background(), endpointID, input, nil)
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

func recordSize(t *testing.T, record map[string]interface{}) int {
	t.Helper()
	encoded, err := json.Marshal(record)
	require.NoError(t, err)
	return len(encoded)
}

func TestRun_CapturedPromptHasCredentialsRedacted(t *testing.T) {
	record := meterCapturedFalCall(t, "fal-ai/flux/dev", `{"images":[{"url":"https://x/i.png","width":512,"height":512}]}`,
		map[string]interface{}{"prompt": "use api_key=" + leakedKey + " to draw a cat"})

	prompt := capturedPromptText(t, record)
	assert.NotContains(t, prompt, "FALSECRET")
	assert.Contains(t, prompt, "REDACTED")
	assert.Contains(t, prompt, "to draw a cat")
}

func TestRun_CapturedPromptAndAnswerHonourTheSizeLimit(t *testing.T) {
	t.Setenv(core.EnvMaxPromptSize, "1000")
	answer := strings.Repeat("a", 5000)
	reply := fmt.Sprintf(`{"choices":[{"finish_reason":"stop","message":{"content":%q}}],"usage":{"prompt_tokens":10,"completion_tokens":5}}`, answer)

	record := meterCapturedFalCall(t, chatEndpoint, reply, map[string]interface{}{"prompt": strings.Repeat("p", 60054)})

	assert.LessOrEqual(t, utf8.RuneCountInString(capturedPromptText(t, record)), 1000)
	assert.LessOrEqual(t, utf8.RuneCountInString(record["outputResponse"].(string)), 1000)
	assert.Equal(t, true, record["promptsTruncated"])
}

func TestRun_CapturedAnswerHonoursTheDefaultSizeLimit(t *testing.T) {
	answer := strings.Repeat("a", 500000)
	reply := fmt.Sprintf(`{"choices":[{"finish_reason":"stop","message":{"content":%q}}],"usage":{"prompt_tokens":10,"completion_tokens":5}}`, answer)

	record := meterCapturedFalCall(t, chatEndpoint, reply, map[string]interface{}{"prompt": "hi"})

	assert.LessOrEqual(t, utf8.RuneCountInString(record["outputResponse"].(string)), 50000)
}

func TestRun_SyncModeImagesNeverPutInlineDataInTheRecord(t *testing.T) {
	inline := "data:image/png;base64," + strings.Repeat("A", 1500000)
	output := fmt.Sprintf(`{"images":[{"url":%q,"width":1024,"height":1024},{"url":%q,"width":1024,"height":1024},{"url":"https://x/linked.png","width":1024,"height":1024}]}`, inline, inline)

	record := meterCapturedFalCall(t, "fal-ai/flux/dev", output, map[string]interface{}{"prompt": "cat", "sync_mode": true})

	assert.NotContains(t, record["outputResponse"], "data:")
	assert.Contains(t, record["outputResponse"], "https://x/linked.png")
	assert.EqualValues(t, 3, record["actualImageCount"])
	assert.Less(t, recordSize(t, record), 10000)
}

func TestRun_SyncModeVideoNeverPutsInlineDataInTheRecord(t *testing.T) {
	inline := "data:video/mp4;base64," + strings.Repeat("A", 100000)
	output := fmt.Sprintf(`{"video":{"url":%q}}`, inline)

	record := meterCapturedFalCall(t, "fal-ai/veo3/fast", output, map[string]interface{}{"prompt": "boat", "sync_mode": true})

	assert.Nil(t, record["outputResponse"])
	assert.Less(t, recordSize(t, record), 10000)
}
