package google

import (
	"context"
	"net/http"
	"testing"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/genai"
)

const (
	geminiNoUsageResponse   = `{"candidates":[{"content":{"role":"model","parts":[{"text":"hi"}]},"finishReason":"STOP"}],"modelVersion":"gemini-test"}`
	geminiNoUsageFirstChunk = `{"candidates":[{"content":{"role":"model","parts":[{"text":"h"}]}}],"modelVersion":"gemini-test"}`
)

var logLevels = []core.LogLevel{core.LogLevelDebug, core.LogLevelInfo, core.LogLevelWarn, core.LogLevelError}

func useLogLevel(t *testing.T, level core.LogLevel) {
	t.Helper()
	previous := core.GetLogger()
	logger := core.NewDefaultLogger()
	logger.SetLevel(level)
	core.SetLogger(logger)
	t.Cleanup(func() { core.SetLogger(previous) })
}

func assertZeroTokenRecord(t *testing.T, record map[string]interface{}, streamed bool) {
	t.Helper()
	assertTokenCounts(t, record, 0, 0, 0, 0)
	assert.Equal(t, "END", record["stopReason"])
	assert.Equal(t, streamed, record["isStreamed"])
	assert.Nil(t, record["errorReason"])
}

func TestReplyWithoutUsageIsReturnedAndRecorded(t *testing.T) {
	for _, provider := range streamProviders {
		for _, level := range logLevels {
			t.Run(provider.String()+" "+level.String()+" non-streamed", func(t *testing.T) {
				useLogLevel(t, level)
				c := newStubbedClient(t, provider, func(w http.ResponseWriter, r *http.Request) {
					writeJSON(w, http.StatusOK, geminiNoUsageResponse)
				})

				var resp *genai.GenerateContentResponse
				var err error
				require.NotPanics(t, func() {
					resp, err = c.client.Models().GenerateContent(context.Background(), "gemini-test", genai.Text("hi"), nil)
				})
				require.NoError(t, err)
				assert.Equal(t, "hi", resp.Text())

				assertZeroTokenRecord(t, singleRecord(t, c), false)
			})
			t.Run(provider.String()+" "+level.String()+" streamed", func(t *testing.T) {
				useLogLevel(t, level)
				c := newStubbedClient(t, provider, streamHandler(geminiNoUsageFirstChunk, geminiNoUsageResponse))

				var text string
				require.NotPanics(t, func() {
					for chunk, err := range c.client.Models().GenerateContentStream(context.Background(), "gemini-test", genai.Text("hi"), nil) {
						require.NoError(t, err)
						text += chunk.Text()
					}
				})
				assert.Equal(t, "hhi", text)

				assertZeroTokenRecord(t, singleRecord(t, c), true)
			})
		}
	}
}

func TestStreamWithoutUsageStoppedByConsumerIsRecorded(t *testing.T) {
	c := newStubbedClient(t, ProviderGoogleAI, streamHandler(geminiNoUsageFirstChunk, geminiNoUsageResponse))

	for _, err := range c.client.Models().GenerateContentStream(context.Background(), "gemini-test", genai.Text("hi"), nil) {
		require.NoError(t, err)
		break
	}

	record := singleRecord(t, c)
	assertTokenCounts(t, record, 0, 0, 0, 0)
	assert.Equal(t, "CANCELLED", record["stopReason"])
	assert.Equal(t, true, record["isStreamed"])
	assert.Nil(t, record["errorReason"])
}
