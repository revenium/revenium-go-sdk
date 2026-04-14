package metering

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewPayload_Defaults(t *testing.T) {
	p := NewPayload(OperationChat, "gpt-4", "OPENAI").Build()

	assert.Equal(t, "END", p.StopReason)
	assert.Equal(t, "AI", p.CostType)
	assert.Equal(t, "CHAT", p.OperationType)
	assert.Equal(t, "gpt-4", p.Model)
	assert.Equal(t, "OPENAI", p.Provider)
	assert.Equal(t, MiddlewareSource, p.MiddlewareSource)
	assert.NotEmpty(t, p.TransactionID)
	assert.Equal(t, int64(0), p.InputTokenCount)
	assert.Equal(t, int64(0), p.OutputTokenCount)
	assert.Equal(t, int64(0), p.ReasoningTokenCount)
}

func TestPayloadBuilder_Chain(t *testing.T) {
	now := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	dur := 500 * time.Millisecond
	firstToken := now.Add(100 * time.Millisecond)

	p := NewPayload(OperationChat, "claude-3", "Anthropic").
		WithTiming(now, dur).
		WithTokens(100, 200, 300).
		WithReasoningTokens(50, 10, 20).
		WithStreaming(true, 100, &firstToken).
		WithStopReason("TOKEN_LIMIT").
		WithModelSource("ANTHROPIC").
		WithSystemFingerprint("fp_abc123").
		WithTemperature(0.7).
		Build()

	assert.Equal(t, "TOKEN_LIMIT", p.StopReason)
	assert.True(t, p.IsStreamed)
	assert.Equal(t, int64(100), p.InputTokenCount)
	assert.Equal(t, int64(200), p.OutputTokenCount)
	assert.Equal(t, int64(300), p.TotalTokenCount)
	assert.Equal(t, int64(50), p.ReasoningTokenCount)
	assert.Equal(t, int64(10), p.CacheCreationTokenCount)
	assert.Equal(t, int64(20), p.CacheReadTokenCount)
	assert.Equal(t, int64(100), p.TimeToFirstToken)
	assert.Equal(t, "fp_abc123", p.SystemFingerprint)
	assert.Equal(t, "ANTHROPIC", p.ModelSource)
	assert.Equal(t, 0.7, *p.Temperature)
	assert.Equal(t, "2025-01-01T12:00:00Z", p.RequestTime)
	assert.Equal(t, "2025-01-01T12:00:00Z", p.CompletionStartTime)
	assert.Equal(t, int64(500), p.RequestDuration)
}

func TestPayloadBuilder_WithError(t *testing.T) {
	p := NewPayload(OperationChat, "gpt-4", "OPENAI").
		WithError("connection timeout").
		Build()

	assert.Equal(t, "ERROR", p.StopReason)
	assert.Equal(t, "connection timeout", p.ErrorReason)
}

func TestPayloadBuilder_JSONWireFormat(t *testing.T) {
	p := NewPayload(OperationChat, "gpt-4", "OPENAI").
		WithTiming(time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC), time.Second).
		WithTokens(10, 20, 30).
		Build()

	data, err := json.Marshal(p)
	require.NoError(t, err)

	var m map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &m))

	assert.Equal(t, "END", m["stopReason"])
	assert.Equal(t, "AI", m["costType"])
	assert.Equal(t, "CHAT", m["operationType"])
	assert.Equal(t, float64(10), m["inputTokenCount"])
	assert.Equal(t, float64(20), m["outputTokenCount"])
	assert.Equal(t, float64(0), m["reasoningTokenCount"])
	assert.Equal(t, float64(0), m["cacheCreationTokenCount"])
	assert.Equal(t, float64(0), m["cacheReadTokenCount"])
	assert.Equal(t, float64(30), m["totalTokenCount"])
	assert.Equal(t, "gpt-4", m["model"])
	assert.Equal(t, "revenium-go-sdk", m["middlewareSource"])
	assert.Equal(t, "2025-01-01T12:00:00Z", m["requestTime"])
	assert.Equal(t, float64(0), m["timeToFirstToken"])

	_, hasSystemFingerprint := m["systemFingerprint"]
	assert.False(t, hasSystemFingerprint, "empty systemFingerprint should be omitted")
}

func TestPayloadBuilder_ImageBilling(t *testing.T) {
	p := NewPayload(OperationImage, "flux/dev", "fal_ai").
		WithImageBilling(4, 4).
		WithAttributes(map[string]interface{}{"width": 1024, "height": 1024}).
		Build()

	assert.Equal(t, "IMAGE", p.OperationType)
	assert.Equal(t, 4, *p.ActualImageCount)
	assert.Equal(t, 4, *p.RequestedImageCount)
	assert.Equal(t, 1024, p.Attributes["width"])
}

func TestPayloadBuilder_VideoDuration(t *testing.T) {
	p := NewPayload(OperationVideo, "gen3a_turbo", "runway").
		WithVideoDuration(5.0, 10.0).
		Build()

	assert.Equal(t, "VIDEO", p.OperationType)
	assert.Equal(t, 5.0, *p.DurationSeconds)
	assert.Equal(t, 10.0, *p.RequestedDurationSeconds)
}
