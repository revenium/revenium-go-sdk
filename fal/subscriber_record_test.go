package fal

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/revenium/revenium-go-sdk/core/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunRecordCarriesTypedSubscriberOverArgumentSubscriber(t *testing.T) {
	falServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"images":[{"url":"https://x/i.png","width":512,"height":512}]}`))
	}))
	defer falServer.Close()
	mock := testutil.NewMockMeteringServer()
	defer mock.Close()

	client, err := NewReveniumFal(&Config{
		FalAPIKey:      "test-key",
		FalBaseURL:     falServer.URL,
		QueueBaseURL:   falServer.URL,
		RequestTimeout: 10 * time.Second,
		Revenium:       &core.ReveniumConfig{APIKey: "hak_test_key_123", BaseURL: mock.URL()},
	})
	require.NoError(t, err)
	defer client.Close()
	ctx := core.WithSubscriber(context.Background(), &core.Subscriber{ID: "rt-sub-42", Email: "rt-sub@example.com"})

	_, err = client.Run(ctx, "fal-ai/flux/schnell", map[string]interface{}{"prompt": "cat"}, map[string]interface{}{
		"subscriber": map[string]interface{}{"id": "argument-user"},
	})
	require.NoError(t, err)
	client.Flush()

	require.True(t, mock.WaitForPayloads(1, 2*time.Second))
	assert.Equal(t, map[string]interface{}{"id": "rt-sub-42", "email": "rt-sub@example.com"}, mock.GetPayloads()[0]["subscriber"])
}
