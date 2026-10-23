package perplexity

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	openai "github.com/openai/openai-go/v3"

	"github.com/revenium/revenium-go-sdk/core/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStreamingWrapper_ClosedTwiceSendsOneRecord(t *testing.T) {
	pplxServer := httptest.NewServer(sseChatHandler(t))
	defer pplxServer.Close()

	mock := testutil.NewMockMeteringServer()
	defer mock.Close()

	r := newTestPerplexity(t, pplxServer.URL, mock.URL())
	defer r.Close()

	stream, err := r.Chat().Completions().NewStreaming(context.Background(), openai.ChatCompletionNewParams{
		Model:    "sonar-small",
		Messages: []openai.ChatCompletionMessageParamUnion{openai.UserMessage("hi")},
	})
	require.NoError(t, err)
	for stream.Next() {
		stream.Current()
	}
	require.NoError(t, stream.Err())

	require.NoError(t, stream.Close())
	require.NoError(t, stream.Close())
	r.Flush()

	require.True(t, mock.WaitForPayloads(1, 2*time.Second))
	time.Sleep(200 * time.Millisecond)
	payloads := mock.GetPayloads()
	require.Len(t, payloads, 1, "one streamed call must produce exactly one usage record")
	assert.Equal(t, float64(7), payloads[0]["inputTokenCount"])
}
