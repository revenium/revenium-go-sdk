package testutil

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// MockMeteringServer is a thread-safe httptest-based fake of the Revenium metering API
// suitable for capturing payloads in unit tests across all middleware modules.
type MockMeteringServer struct {
	server *httptest.Server

	mu       sync.RWMutex
	payloads []map[string]interface{}
	requests []CapturedRequest

	failureStatus atomic.Int32
	failureBody   atomic.Value
	latency       atomic.Int64
}

// CapturedRequest describes a single HTTP request received by the mock server.
type CapturedRequest struct {
	Path    string
	Method  string
	Headers http.Header
	Payload map[string]interface{}
}

// NewMockMeteringServer starts a new mock metering server. Caller must invoke Close().
func NewMockMeteringServer() *MockMeteringServer {
	m := &MockMeteringServer{}
	m.failureBody.Store("")
	m.server = httptest.NewServer(http.HandlerFunc(m.handle))
	return m
}

// URL returns the base URL of the running mock server.
func (m *MockMeteringServer) URL() string {
	return m.server.URL
}

// Close shuts down the underlying httptest server.
func (m *MockMeteringServer) Close() {
	m.server.Close()
}

// GetPayloads returns a copy of all metering payloads captured so far.
func (m *MockMeteringServer) GetPayloads() []map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]map[string]interface{}, len(m.payloads))
	copy(out, m.payloads)
	return out
}

// GetRequests returns a copy of all requests captured so far, including non-payload metadata.
func (m *MockMeteringServer) GetRequests() []CapturedRequest {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]CapturedRequest, len(m.requests))
	copy(out, m.requests)
	return out
}

// Reset clears captured state without restarting the server.
func (m *MockMeteringServer) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.payloads = nil
	m.requests = nil
	m.failureStatus.Store(0)
	m.failureBody.Store("")
	m.latency.Store(0)
}

// SetFailure makes subsequent requests respond with the given status and body.
// Pass status=0 to clear the failure mode.
func (m *MockMeteringServer) SetFailure(status int, body string) {
	m.failureStatus.Store(int32(status))
	m.failureBody.Store(body)
}

// SetLatency adds an artificial delay to every response.
func (m *MockMeteringServer) SetLatency(d time.Duration) {
	m.latency.Store(int64(d))
}

// WaitForPayloads blocks up to timeout until at least n payloads have been captured.
// Returns true if the count was reached, false on timeout.
func (m *MockMeteringServer) WaitForPayloads(n int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if len(m.GetPayloads()) >= n {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return len(m.GetPayloads()) >= n
}

func (m *MockMeteringServer) handle(w http.ResponseWriter, r *http.Request) {
	if d := time.Duration(m.latency.Load()); d > 0 {
		time.Sleep(d)
	}

	body, _ := io.ReadAll(r.Body)
	defer r.Body.Close()

	captured := CapturedRequest{
		Path:    r.URL.Path,
		Method:  r.Method,
		Headers: r.Header.Clone(),
	}

	if len(body) > 0 && strings.Contains(r.Header.Get("Content-Type"), "json") {
		parsed := map[string]interface{}{}
		if err := json.Unmarshal(body, &parsed); err == nil {
			captured.Payload = parsed
			m.mu.Lock()
			m.payloads = append(m.payloads, parsed)
			m.requests = append(m.requests, captured)
			m.mu.Unlock()
		} else {
			m.mu.Lock()
			m.requests = append(m.requests, captured)
			m.mu.Unlock()
		}
	} else {
		m.mu.Lock()
		m.requests = append(m.requests, captured)
		m.mu.Unlock()
	}

	if status := m.failureStatus.Load(); status != 0 {
		w.WriteHeader(int(status))
		failBody, _ := m.failureBody.Load().(string)
		_, _ = w.Write([]byte(failBody))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}
