package resilience

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const clientTimeout = 50 * time.Millisecond

func slowServer(t *testing.T, hits *atomic.Int32, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hits != nil {
			hits.Add(1)
		}
		handler(w, r)
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(func() {
		close(release)
		server.Close()
	})
	return server
}

func headersNeverArrive(http.ResponseWriter, *http.Request) {}

func bodyNeverEnds(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Length", "100")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("partial"))
	w.(http.Flusher).Flush()
}

func timedOutCall(t *testing.T, server *httptest.Server) error {
	t.Helper()
	client := &http.Client{Timeout: clientTimeout}
	resp, err := client.Get(server.URL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, err = io.ReadAll(resp.Body)
	require.Error(t, err, "the body read did not time out")
	return err
}

func TestClassifyError_HTTPClientTimeoutWhileAwaitingHeadersIsNotRetried(t *testing.T) {
	err := timedOutCall(t, slowServer(t, nil, headersNeverArrive))

	assert.Equal(t, ClassificationNonRetryable, ClassifyError(err), "error: %v", err)
}

func TestClassifyError_HTTPClientTimeoutWhileReadingTheBodyIsNotRetried(t *testing.T) {
	err := timedOutCall(t, slowServer(t, nil, bodyNeverEnds))

	assert.Equal(t, ClassificationNonRetryable, ClassifyError(err), "error: %v", err)
}

func TestClassifyError_WrappedHTTPClientTimeoutIsNotRetried(t *testing.T) {
	err := timedOutCall(t, slowServer(t, nil, headersNeverArrive))

	assert.Equal(t, ClassificationNonRetryable, ClassifyError(core.NewNetworkError("request failed", err)))
}

func TestRetryThroughBreaker_SendsAnHTTPClientTimeoutOnce(t *testing.T) {
	var hits atomic.Int32
	server := slowServer(t, &hits, headersNeverArrive)
	client := &http.Client{Timeout: clientTimeout}
	config := &RetryConfig{MaxRetries: 2, BaseDelay: time.Millisecond, MaxDelay: 5 * time.Millisecond}

	err := RetryThroughBreaker(NewCircuitBreaker(CircuitBreakerConfig{}), nil, config, func() error {
		resp, err := client.Get(server.URL)
		if err == nil {
			resp.Body.Close()
		}
		return err
	})

	require.Error(t, err)
	assert.Equal(t, int32(1), hits.Load())
}

type networkTimeout struct{}

func (networkTimeout) Error() string   { return "i/o timeout" }
func (networkTimeout) Timeout() bool   { return true }
func (networkTimeout) Temporary() bool { return true }

func TestClassifyError_NetworkTimeoutOutsideTheHTTPClientIsStillRetried(t *testing.T) {
	dial := &url.Error{Op: "Post", URL: "https://api.dev.example/meter", Err: &net.OpError{Op: "dial", Net: "tcp", Err: networkTimeout{}}}

	assert.Equal(t, ClassificationRetryable, ClassifyError(dial))
	assert.False(t, errors.Is(dial, context.DeadlineExceeded))
}

func TestClassifyError_CallerDeadlineIsNotRetried(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), clientTimeout)
	defer cancel()
	server := slowServer(t, nil, headersNeverArrive)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
	require.NoError(t, err)

	_, err = http.DefaultClient.Do(req)

	assert.Equal(t, ClassificationNonRetryable, ClassifyError(err))
}
