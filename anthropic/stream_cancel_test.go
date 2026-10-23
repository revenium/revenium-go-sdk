package anthropic

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/revenium/revenium-go-sdk/core"
)

const firstTextDelta = 3

func readEvents(t *testing.T, stream interface{}, count int) *StreamingWrapper {
	t.Helper()
	wrapper, ok := stream.(*StreamingWrapper)
	require.True(t, ok)
	for read := 0; read < count; read++ {
		require.True(t, wrapper.Next())
	}
	return wrapper
}

func TestMessagesStream_ClosedBeforeTheStopEventIsRecordedAsCancelled(t *testing.T) {
	t.Setenv(core.EnvCapturePrompts, "true")
	serveSSE(t, toolUseStreamEvents, "")
	r, mock := newMeteredAnthropic(t)

	stream, err := r.Messages().CreateMessageStream(context.Background(), capturedMessageParams())
	require.NoError(t, err)
	require.NoError(t, readEvents(t, stream, firstTextDelta).Close())

	record := flushedRecords(t, r, mock, 1)[0]
	assert.Equal(t, "CANCELLED", record["stopReason"])
	assert.Equal(t, float64(12), record["inputTokenCount"])
	assert.Equal(t, float64(1), record["outputTokenCount"], "only message_start's count is known")
	assert.Equal(t, true, record["promptsTruncated"])
}

func TestMessagesStream_ClosedRightAfterTheStopEventKeepsTheProviderStopReason(t *testing.T) {
	serveSSE(t, toolUseStreamEvents, "")
	r, mock := newMeteredAnthropic(t)

	stream, err := r.Messages().CreateMessageStream(context.Background(), capturedMessageParams())
	require.NoError(t, err)
	require.NoError(t, readEvents(t, stream, len(toolUseStreamEvents)).Close())

	record := flushedRecords(t, r, mock, 1)[0]
	assert.Equal(t, "END_SEQUENCE", record["stopReason"])
	assert.Equal(t, float64(9), record["outputTokenCount"])
}

func TestBetaMessagesStream_ClosedBeforeTheStopEventIsRecordedAsCancelled(t *testing.T) {
	t.Setenv(core.EnvCapturePrompts, "true")
	serveSSE(t, toolUseStreamEvents, "")
	r, mock := newMeteredAnthropic(t)

	stream, err := r.BetaMessages().CreateMessageStream(context.Background(), betaParams())
	require.NoError(t, err)
	for read := 0; read < firstTextDelta; read++ {
		require.True(t, stream.Next())
	}
	require.NoError(t, stream.Close())

	record := flushedRecords(t, r, mock, 1)[0]
	assert.Equal(t, "CANCELLED", record["stopReason"])
	assert.Equal(t, float64(12), record["inputTokenCount"])
	assert.Equal(t, true, record["promptsTruncated"])
}

func TestBetaMessagesStream_ClosedRightAfterTheStopEventKeepsTheProviderStopReason(t *testing.T) {
	serveSSE(t, toolUseStreamEvents, "")
	r, mock := newMeteredAnthropic(t)

	stream, err := r.BetaMessages().CreateMessageStream(context.Background(), betaParams())
	require.NoError(t, err)
	for range toolUseStreamEvents {
		require.True(t, stream.Next())
	}
	require.NoError(t, stream.Close())

	assert.Equal(t, "END_SEQUENCE", flushedRecords(t, r, mock, 1)[0]["stopReason"])
}

func TestBetaToolRunnerStreaming_TurnAbandonedBeforeTheStopEventIsRecordedAsCancelled(t *testing.T) {
	t.Setenv(core.EnvCapturePrompts, "true")
	newBetaProviderStub(t, betaFinalMessage)
	r, mock := newMeteredAnthropic(t)

	runner := r.BetaMessages().NewToolRunnerStreaming(nil, runnerParams())
	for events, err := range runner.AllStreaming(context.Background()) {
		require.NoError(t, err)
		for event, eventErr := range events {
			require.NoError(t, eventErr)
			if event.Type == "content_block_delta" {
				break
			}
		}
		break
	}

	record := flushedRecords(t, r, mock, 1)[0]
	assert.Equal(t, "CANCELLED", record["stopReason"])
	assert.Equal(t, float64(30), record["inputTokenCount"])
	assert.Equal(t, true, record["promptsTruncated"])
}

func TestBedrockInvokeModelStream_ClosedBeforeTheStopEventIsRecordedAsCancelled(t *testing.T) {
	t.Setenv(core.EnvCapturePrompts, "true")
	stub := newBedrockStub(t)
	stub.streamWith(invokeStreamChunks(
		invokeStreamMessageStartWithoutCache, invokeStreamBlockStart, invokeStreamBlockDelta, invokeStreamBlockStop,
		invokeStreamOutputOnly, invokeStreamStopWithoutCache,
	))
	mock := newMeteringMock(t)
	r := newBedrockAnthropic(t, bedrockConfig(mock.URL()))

	stream, err := r.Messages().CreateMessageStream(context.Background(), messageParams(streamedModelID))
	require.NoError(t, err)
	require.NoError(t, readEvents(t, stream, firstTextDelta).Close())

	record := onlyRecord(t, r, mock)
	assert.Equal(t, "CANCELLED", record["stopReason"])
	assert.Equal(t, float64(1), record["inputTokenCount"])
	assert.Equal(t, float64(2), record["outputTokenCount"])
	assert.Equal(t, true, record["promptsTruncated"])
}

func TestBedrockInvokeModelStream_ClosedRightAfterTheStopEventKeepsTheProviderStopReason(t *testing.T) {
	events := []string{
		invokeStreamMessageStartWithoutCache, invokeStreamBlockStart, invokeStreamBlockDelta, invokeStreamBlockStop,
		invokeStreamOutputOnly, invokeStreamStopWithoutCache,
	}
	stub := newBedrockStub(t)
	stub.streamWith(invokeStreamChunks(events...))
	mock := newMeteringMock(t)
	r := newBedrockAnthropic(t, bedrockConfig(mock.URL()))

	stream, err := r.Messages().CreateMessageStream(context.Background(), messageParams(streamedModelID))
	require.NoError(t, err)
	require.NoError(t, readEvents(t, stream, len(events)).Close())

	assert.Equal(t, "END", onlyRecord(t, r, mock)["stopReason"])
}

func TestBedrockConverseStream_ClosedBeforeTheStopEventIsRecordedAsCancelled(t *testing.T) {
	t.Setenv(core.EnvCapturePrompts, "true")
	stub := newBedrockStub(t)
	stub.streamWith(converseStopEvents("end_turn"))
	mock := newMeteringMock(t)
	r := newConverseAnthropic(t, mock.URL())

	stream, err := r.Messages().CreateMessageStream(context.Background(), messageParams("claude-haiku-4-5-20251001"))
	require.NoError(t, err)
	require.NoError(t, readEvents(t, stream, 2).Close())

	record := onlyRecord(t, r, mock)
	assert.Equal(t, "CANCELLED", record["stopReason"])
	assert.Equal(t, true, record["promptsTruncated"])
}

func TestBedrockConverseStream_ClosedAfterTheStopEventKeepsTheProviderStopReason(t *testing.T) {
	stub := newBedrockStub(t)
	stub.streamWith(converseStopEvents("end_turn"))
	mock := newMeteringMock(t)
	r := newConverseAnthropic(t, mock.URL())

	stream, err := r.Messages().CreateMessageStream(context.Background(), messageParams("claude-haiku-4-5-20251001"))
	require.NoError(t, err)
	require.NoError(t, readEvents(t, stream, 4).Close())

	assert.Equal(t, "END", onlyRecord(t, r, mock)["stopReason"])
}

func TestStreamsEndingWithoutAStopEventKeepEndAndMarkTheReplyCut(t *testing.T) {
	surfaces := map[string]func(t *testing.T, r *ReveniumAnthropic){
		"messages stream": func(t *testing.T, r *ReveniumAnthropic) {
			stream, err := r.Messages().CreateMessageStream(context.Background(), capturedMessageParams())
			require.NoError(t, err)
			drainStream(t, stream)
		},
		"beta messages stream": func(t *testing.T, r *ReveniumAnthropic) {
			stream, err := r.BetaMessages().CreateMessageStream(context.Background(), betaParams())
			require.NoError(t, err)
			for stream.Next() {
			}
			require.NoError(t, stream.Err())
			require.NoError(t, stream.Close())
		},
	}
	for name, read := range surfaces {
		t.Run(name, func(t *testing.T) {
			t.Setenv(core.EnvCapturePrompts, "true")
			serveSSE(t, toolUseStreamEvents[:firstTextDelta], "")
			r, mock := newMeteredAnthropic(t)

			read(t, r)

			record := flushedRecords(t, r, mock, 1)[0]
			assert.Equal(t, "END", record["stopReason"])
			assert.Equal(t, true, record["promptsTruncated"])
		})
	}
}
