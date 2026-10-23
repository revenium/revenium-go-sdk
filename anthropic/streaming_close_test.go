package anthropic

import (
	"testing"
	"time"

	"github.com/revenium/revenium-go-sdk/core/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type countingProviderStream struct {
	closes int
}

func (s *countingProviderStream) Close() error {
	s.closes++
	return nil
}

func TestStreamingWrapper_ClosedTwiceSendsOneRecord(t *testing.T) {
	mock := testutil.NewMockMeteringServer()
	defer mock.Close()

	r := newTestAnthropic(t, mock.URL())
	defer r.Close()

	providerStream := &countingProviderStream{}
	w := &StreamingWrapper{
		stream:       providerStream,
		config:       r.config,
		startTime:    time.Now(),
		messagesAPI:  &MessagesInterface{parent: r},
		model:        "claude-haiku-4-5",
		provider:     "anthropic",
		inputTokens:  12,
		outputTokens: 4,
		totalTokens:  16,
	}

	require.NoError(t, w.Close())
	require.NoError(t, w.Close())
	r.Flush()

	require.True(t, mock.WaitForPayloads(1, 2*time.Second))
	time.Sleep(200 * time.Millisecond)
	payloads := mock.GetPayloads()
	require.Len(t, payloads, 1, "one streamed call must produce exactly one usage record")
	assert.Equal(t, float64(12), payloads[0]["inputTokenCount"])
	assert.Equal(t, 1, providerStream.closes, "the provider stream must be closed once")
}
