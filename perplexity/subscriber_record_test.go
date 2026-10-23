package perplexity

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	openai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/shared/constant"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/revenium/revenium-go-sdk/core/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChatRecordCarriesTypedSubscriber(t *testing.T) {
	providerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"pplx-sub","object":"chat.completion","created":1700000000,"model":"sonar","choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":2,"total_tokens":12}}`))
	}))
	defer providerServer.Close()
	mock := testutil.NewMockMeteringServer()
	defer mock.Close()

	r := newTestPerplexity(t, providerServer.URL, mock.URL())
	defer r.Close()
	ctx := core.WithSubscriber(context.Background(), &core.Subscriber{ID: "rt-sub-42", Email: "rt-sub@example.com"})

	_, err := r.Chat().Completions().New(ctx, openai.ChatCompletionNewParams{
		Model: "sonar",
		Messages: []openai.ChatCompletionMessageParamUnion{
			{OfUser: &openai.ChatCompletionUserMessageParam{
				Role:    constant.ValueOf[constant.User](),
				Content: openai.ChatCompletionUserMessageParamContentUnion{OfString: openai.String("hi")},
			}},
		},
	})
	require.NoError(t, err)
	r.Flush()

	require.True(t, mock.WaitForPayloads(1, 2*time.Second))
	assert.Equal(t, map[string]interface{}{"id": "rt-sub-42", "email": "rt-sub@example.com"}, mock.GetPayloads()[0]["subscriber"])
}
