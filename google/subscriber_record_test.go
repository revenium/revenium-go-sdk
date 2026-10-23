package google

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/revenium/revenium-go-sdk/core/enforcement"
	"github.com/revenium/revenium-go-sdk/core/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/genai"
)

func meteredGenerateContentPayload(t *testing.T, ctx context.Context) map[string]interface{} {
	t.Helper()
	geminiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"candidates":[{"content":{"role":"model","parts":[{"text":"hi"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":2,"totalTokenCount":12},"modelVersion":"gemini-2.0-flash"}`))
	}))
	defer geminiServer.Close()
	t.Setenv("GOOGLE_GEMINI_BASE_URL", geminiServer.URL)
	mock := testutil.NewMockMeteringServer()
	defer mock.Close()

	r, err := NewReveniumGoogle(&Config{
		GoogleAPIKey:   "test-google-key",
		VertexDisabled: true,
		Revenium: &core.ReveniumConfig{
			APIKey:  "hak_test_key_123",
			BaseURL: mock.URL(),
		},
	})
	require.NoError(t, err)
	defer r.Close()

	_, err = r.Models().GenerateContent(ctx, "gemini-2.0-flash", genai.Text("hi"), nil)
	require.NoError(t, err)
	r.Flush()

	require.True(t, mock.WaitForPayloads(1, 2*time.Second))
	return mock.GetPayloads()[0]
}

func TestGenerateContentRecordCarriesTypedSubscriber(t *testing.T) {
	ctx := core.WithSubscriber(context.Background(), &core.Subscriber{ID: "rt-sub-42", Email: "rt-sub@example.com"})

	payload := meteredGenerateContentPayload(t, ctx)

	assert.Equal(t, map[string]interface{}{"id": "rt-sub-42", "email": "rt-sub@example.com"}, payload["subscriber"])
}

func TestGenerateContentCostControlAndRecordUseTheSameSubscriber(t *testing.T) {
	ctx := core.WithUsageMetadata(context.Background(), map[string]interface{}{
		"subscriber": map[string]interface{}{"id": "map-user"},
	})
	ctx = core.WithSubscriber(ctx, &core.Subscriber{ID: "typed-user"})

	payload := meteredGenerateContentPayload(t, ctx)
	recordSubscriber, ok := payload["subscriber"].(map[string]interface{})
	require.True(t, ok)

	assert.Equal(t, "typed-user", recordSubscriber["id"])
	assert.Equal(t, recordSubscriber["id"], enforcement.NewEvalContext(ctx, "gemini-2.0-flash", ProviderGoogleAI.String()).SubscriberID)
}
