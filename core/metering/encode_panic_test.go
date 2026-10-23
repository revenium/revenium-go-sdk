package metering

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type panickingMarshaler struct{}

func (panickingMarshaler) MarshalJSON() ([]byte, error) {
	panic("attribute encoder failed")
}

func errorLinesContaining(logger *recordingLogger, text string) int {
	count := 0
	for _, line := range logger.Lines() {
		if strings.HasPrefix(line, "ERROR") && strings.Contains(line, text) {
			count++
		}
	}
	return count
}

func TestSend_ARecordWhoseEncodingPanicsIsDroppedWithoutReachingTheCaller(t *testing.T) {
	logger := useRecordingLogger(t)
	srv, requests := recordingServer(t)
	client := newTestClient(t, srv.URL)
	t.Cleanup(func() { _ = client.Close() })
	payload := NewPayload(OperationChat, "gpt-4", "OPENAI").
		WithAttributes(map[string]interface{}{"broken": panickingMarshaler{}}).
		Build()

	require.NotPanics(t, func() { client.Send(payload) })
	client.Flush()

	assert.Empty(t, requests(), "the record that could not be encoded is dropped")
	assert.Equal(t, 1, errorLinesContaining(logger, "attribute encoder failed"))
	assert.Zero(t, client.GetBufferStats().HeldBytes)
}

func TestSendToolEvent_AnEventWhoseEncodingPanicsIsDroppedWithoutReachingTheCaller(t *testing.T) {
	logger := useRecordingLogger(t)
	srv, requests := recordingServer(t)
	client := newTestClient(t, srv.URL)
	t.Cleanup(func() { _ = client.Close() })
	payload := &ToolEventPayload{UsageMetadata: map[string]interface{}{"broken": panickingMarshaler{}}}

	require.NotPanics(t, func() { client.SendToolEvent(payload) })
	client.Flush()

	assert.Empty(t, requests(), "the event that could not be encoded is dropped")
	assert.Equal(t, 1, errorLinesContaining(logger, "attribute encoder failed"))
}
