package openai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	openaisdk "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/shared/constant"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/revenium/revenium-go-sdk/core/enforcement"
	"github.com/revenium/revenium-go-sdk/core/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func chatCompletionHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl-sub","object":"chat.completion","created":1700000000,"model":"gpt-4.1-nano","choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":2,"total_tokens":12}}`))
	})
}

func meteredChatPayload(t *testing.T, ctx context.Context) map[string]interface{} {
	t.Helper()
	openaiServer := httptest.NewServer(chatCompletionHandler())
	defer openaiServer.Close()
	mock := testutil.NewMockMeteringServer()
	defer mock.Close()

	r := newTestOpenAI(t, openaiServer.URL, mock.URL())
	defer r.Close()

	_, err := r.Chat().Completions().New(ctx, openaisdk.ChatCompletionNewParams{
		Model: "gpt-4.1-nano",
		Messages: []openaisdk.ChatCompletionMessageParamUnion{
			{OfUser: &openaisdk.ChatCompletionUserMessageParam{
				Role:    constant.ValueOf[constant.User](),
				Content: openaisdk.ChatCompletionUserMessageParamContentUnion{OfString: openaisdk.String("hi")},
			}},
		},
	})
	require.NoError(t, err)
	r.Flush()

	require.True(t, mock.WaitForPayloads(1, 2*time.Second))
	return mock.GetPayloads()[0]
}

func TestChatRecordCarriesTypedSubscriber(t *testing.T) {
	ctx := core.WithSubscriber(context.Background(), &core.Subscriber{ID: "rt-sub-42", Email: "rt-sub@example.com"})

	payload := meteredChatPayload(t, ctx)

	assert.Equal(t, map[string]interface{}{"id": "rt-sub-42", "email": "rt-sub@example.com"}, payload["subscriber"])
}

func TestChatRecordPrefersTypedSubscriberOverMetadataSubscriber(t *testing.T) {
	ctx := core.WithUsageMetadata(context.Background(), map[string]interface{}{
		"subscriber": map[string]interface{}{"id": "map-user"},
	})
	ctx = core.WithSubscriber(ctx, &core.Subscriber{ID: "typed-user"})

	payload := meteredChatPayload(t, ctx)

	assert.Equal(t, map[string]interface{}{"id": "typed-user"}, payload["subscriber"])
}

func TestCostControlAndRecordUseTheSameSubscriber(t *testing.T) {
	cases := map[string]context.Context{
		"typed only": core.WithSubscriber(context.Background(), &core.Subscriber{ID: "typed-user"}),
		"metadata only": core.WithUsageMetadata(context.Background(), map[string]interface{}{
			"subscriber": map[string]interface{}{"id": "map-user"},
		}),
		"both": core.WithSubscriber(core.WithUsageMetadata(context.Background(), map[string]interface{}{
			"subscriber": map[string]interface{}{"id": "map-user"},
		}), &core.Subscriber{ID: "typed-user"}),
	}
	for name, ctx := range cases {
		t.Run(name, func(t *testing.T) {
			payload := meteredChatPayload(t, ctx)
			recordSubscriber, ok := payload["subscriber"].(map[string]interface{})
			require.True(t, ok)

			evalContext := enforcement.NewEvalContext(ctx, "gpt-4.1-nano", "OPENAI")

			assert.Equal(t, recordSubscriber["id"], evalContext.SubscriberID)
		})
	}
}
