package metering

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/revenium/revenium-go-sdk/core/resilience"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var testRetryConfig = &resilience.RetryConfig{
	MaxRetries:   3,
	BaseDelay:    1 * time.Millisecond,
	MaxDelay:     10 * time.Millisecond,
	JitterFactor: 0,
}

func newTestClient(t *testing.T, baseURL string) *MeteringClient {
	t.Helper()
	client, err := NewMeteringClient(MeteringClientConfig{APIKey: "test-key", BaseURL: baseURL})
	require.NoError(t, err)
	client.retryConfig = testRetryConfig
	return client
}

func TestNewMeteringClient_RequiresAPIKey(t *testing.T) {
	_, err := NewMeteringClient(MeteringClientConfig{})
	require.Error(t, err)
}

func TestMeteringClient_SendSync_Success(t *testing.T) {
	var receivedPayload map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "application/json; charset=utf-8", r.Header.Get("Content-Type"))
		assert.Equal(t, "test-key", r.Header.Get("x-api-key"))
		assert.Equal(t, "revenium-go-sdk/1.0", r.Header.Get("User-Agent"))

		_ = json.NewDecoder(r.Body).Decode(&receivedPayload)
		w.WriteHeader(200)
	}))
	defer srv.Close()

	client := newTestClient(t, srv.URL)

	payload := NewPayload(OperationChat, "gpt-4", "OPENAI").Build()
	err := client.SendSync(payload)
	require.NoError(t, err)

	assert.Equal(t, "gpt-4", receivedPayload["model"])
	assert.Equal(t, "revenium-go-sdk", receivedPayload["middlewareSource"])
}

func TestMeteringClient_Retry_OnServerError(t *testing.T) {
	resilience.ResetGlobalCircuitBreaker()
	defer resilience.ResetGlobalCircuitBreaker()

	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := atomic.AddInt32(&attempts, 1)
		if count < 3 {
			w.WriteHeader(500)
			return
		}
		w.WriteHeader(200)
	}))
	defer srv.Close()

	client := newTestClient(t, srv.URL)

	payload := NewPayload(OperationChat, "gpt-4", "OPENAI").Build()
	err := client.SendSync(payload)
	require.NoError(t, err)
	assert.Equal(t, int32(3), atomic.LoadInt32(&attempts))
}

func TestMeteringClient_NoRetry_OnValidationError(t *testing.T) {
	resilience.ResetGlobalCircuitBreaker()
	defer resilience.ResetGlobalCircuitBreaker()

	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(400)
		_, _ = w.Write([]byte(`{"error":"bad request"}`))
	}))
	defer srv.Close()

	client := newTestClient(t, srv.URL)

	payload := NewPayload(OperationChat, "gpt-4", "OPENAI").Build()
	err := client.SendSync(payload)
	require.Error(t, err)
	assert.Equal(t, int32(1), atomic.LoadInt32(&attempts))
}

func TestMeteringClient_Retry_OnThrottled(t *testing.T) {
	resilience.ResetGlobalCircuitBreaker()
	defer resilience.ResetGlobalCircuitBreaker()

	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := atomic.AddInt32(&attempts, 1)
		if count < 2 {
			w.WriteHeader(429)
			_, _ = w.Write([]byte(`{"error":"rate limited"}`))
			return
		}
		w.WriteHeader(200)
	}))
	defer srv.Close()

	client := newTestClient(t, srv.URL)

	payload := NewPayload(OperationChat, "gpt-4", "OPENAI").Build()
	err := client.SendSync(payload)
	require.NoError(t, err)
	assert.Equal(t, int32(2), atomic.LoadInt32(&attempts))
}

func TestMeteringClient_Send_Async(t *testing.T) {
	var called int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&called, 1)
		w.WriteHeader(200)
	}))
	defer srv.Close()

	client := newTestClient(t, srv.URL)

	payload := NewPayload(OperationChat, "gpt-4", "OPENAI").Build()
	client.Send(payload)
	client.Flush()

	assert.Equal(t, int32(1), atomic.LoadInt32(&called))
}

func TestMeteringClient_RoutesEndpointByOperationType(t *testing.T) {
	tests := []struct {
		op       OperationType
		wantPath string
	}{
		{OperationChat, "/meter/v2/ai/completions"},
		{OperationImage, "/meter/v2/ai/images"},
		{OperationVideo, "/meter/v2/ai/video"},
		{OperationAudio, "/meter/v2/ai/audio"},
	}

	for _, tt := range tests {
		t.Run(string(tt.op), func(t *testing.T) {
			var gotPath string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotPath = r.URL.Path
				w.WriteHeader(200)
			}))
			defer srv.Close()

			client := newTestClient(t, srv.URL)
			payload := NewPayload(tt.op, "model", "provider").Build()
			_ = client.SendSync(payload)
			assert.Equal(t, tt.wantPath, gotPath)
		})
	}
}
