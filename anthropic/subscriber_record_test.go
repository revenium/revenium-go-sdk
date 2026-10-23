package anthropic

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	anthropicsdk "github.com/anthropics/anthropic-sdk-go"
	"github.com/revenium/revenium-go-sdk/core"
	"github.com/revenium/revenium-go-sdk/core/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMessageRecordCarriesTypedSubscriber(t *testing.T) {
	anthropicServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg_sub","type":"message","role":"assistant","model":"claude-haiku-4-5","content":[{"type":"text","text":"hi"}],"stop_reason":"end_turn","usage":{"input_tokens":10,"output_tokens":2}}`))
	}))
	defer anthropicServer.Close()
	t.Setenv("ANTHROPIC_BASE_URL", anthropicServer.URL)
	mock := testutil.NewMockMeteringServer()
	defer mock.Close()

	r, err := NewReveniumAnthropic(&Config{
		AnthropicAPIKey: "sk-ant-test",
		BedrockDisabled: true,
		Revenium: &core.ReveniumConfig{
			APIKey:  "hak_test_key_123",
			BaseURL: mock.URL(),
		},
	})
	require.NoError(t, err)
	defer r.Close()
	ctx := core.WithSubscriber(context.Background(), &core.Subscriber{ID: "rt-sub-42", Email: "rt-sub@example.com"})

	_, err = r.Messages().CreateMessage(ctx, anthropicsdk.MessageNewParams{
		Model:     "claude-haiku-4-5",
		MaxTokens: 16,
		Messages:  []anthropicsdk.MessageParam{anthropicsdk.NewUserMessage(anthropicsdk.NewTextBlock("hi"))},
	})
	require.NoError(t, err)
	r.Flush()

	require.True(t, mock.WaitForPayloads(1, 2*time.Second))
	assert.Equal(t, map[string]interface{}{"id": "rt-sub-42", "email": "rt-sub@example.com"}, mock.GetPayloads()[0]["subscriber"])
}
