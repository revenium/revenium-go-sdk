package anthropic

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws/protocol/eventstream"
	"github.com/stretchr/testify/require"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/revenium/revenium-go-sdk/core/testutil"
)

type bedrockRequest struct {
	path          string
	body          []byte
	securityToken string
}

type bedrockEvent struct {
	eventType string
	payload   string
}

type bedrockStub struct {
	server *httptest.Server

	mu       sync.Mutex
	requests []bedrockRequest

	status       int
	errorType    string
	responseBody string
	streamEvents []bedrockEvent
}

var bedrockClaudeBodyFields = map[string]bool{
	"anthropic_version": true, "anthropic_beta": true, "max_tokens": true, "system": true, "messages": true,
	"temperature": true, "top_p": true, "top_k": true, "tools": true, "tool_choice": true,
	"stop_sequences": true, "thinking": true, "output_config": true,
}

func extraneousInvokeModelKey(path string, body []byte) string {
	if !strings.HasSuffix(path, "/invoke") && !strings.HasSuffix(path, "/invoke-with-response-stream") {
		return ""
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(body, &fields) != nil {
		return ""
	}
	for key := range fields {
		if !bedrockClaudeBodyFields[key] {
			return key
		}
	}
	return ""
}

func newBedrockStub(t *testing.T) *bedrockStub {
	t.Helper()
	stub := &bedrockStub{status: http.StatusOK}
	stub.server = httptest.NewServer(http.HandlerFunc(stub.serve))
	t.Cleanup(stub.server.Close)
	isolateAWSEnvironment(t, stub.server.URL)
	return stub
}

func isolateAWSEnvironment(t *testing.T, endpoint string) {
	t.Helper()
	missing := filepath.Join(t.TempDir(), "missing")
	t.Setenv("AWS_ENDPOINT_URL_BEDROCK_RUNTIME", endpoint)
	t.Setenv("AWS_CONFIG_FILE", missing)
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", missing)
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	t.Setenv("AWS_PROFILE", "")
	t.Setenv("AWS_MODEL_ARN_ID", "")
	t.Setenv("AWS_MAX_ATTEMPTS", "1")
}

func (s *bedrockStub) serve(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	s.mu.Lock()
	s.requests = append(s.requests, bedrockRequest{path: r.URL.Path, body: body, securityToken: r.Header.Get("X-Amz-Security-Token")})
	status, errorType, responseBody, events := s.status, s.errorType, s.responseBody, s.streamEvents
	s.mu.Unlock()

	if key := extraneousInvokeModelKey(r.URL.Path, body); key != "" {
		w.Header().Set("X-Amzn-ErrorType", "ValidationException")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"message":"extraneous key [` + key + `] is not permitted"}`))
		return
	}
	if status != http.StatusOK {
		w.Header().Set("X-Amzn-ErrorType", errorType)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(`{"message":"The security token included in the request is invalid."}`))
		return
	}
	if strings.HasSuffix(r.URL.Path, "-stream") {
		writeEventStream(w, events)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(responseBody))
}

func writeEventStream(w http.ResponseWriter, events []bedrockEvent) {
	w.Header().Set("Content-Type", "application/vnd.amazon.eventstream")
	w.WriteHeader(http.StatusOK)
	encoder := eventstream.NewEncoder()
	for _, event := range events {
		var headers eventstream.Headers
		headers.Set(":message-type", eventstream.StringValue("event"))
		headers.Set(":event-type", eventstream.StringValue(event.eventType))
		headers.Set(":content-type", eventstream.StringValue("application/json"))
		var frame bytes.Buffer
		_ = encoder.Encode(&frame, eventstream.Message{Headers: headers, Payload: []byte(event.payload)})
		_, _ = w.Write(frame.Bytes())
	}
}

func invokeStreamChunks(anthropicEvents ...string) []bedrockEvent {
	events := make([]bedrockEvent, 0, len(anthropicEvents))
	for _, event := range anthropicEvents {
		encoded, _ := json.Marshal(map[string]string{"bytes": base64.StdEncoding.EncodeToString([]byte(event))})
		events = append(events, bedrockEvent{eventType: "chunk", payload: string(encoded)})
	}
	return events
}

func (s *bedrockStub) respondWith(body string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.responseBody = body
}

func (s *bedrockStub) streamWith(events []bedrockEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.streamEvents = events
}

func (s *bedrockStub) rejectWith(status int, errorType string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status = status
	s.errorType = errorType
}

func (s *bedrockStub) received() []bedrockRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]bedrockRequest(nil), s.requests...)
}

func (s *bedrockStub) onlyRequest(t *testing.T) bedrockRequest {
	t.Helper()
	requests := s.received()
	require.Len(t, requests, 1)
	return requests[0]
}

func (r bedrockRequest) jsonBody(t *testing.T) map[string]interface{} {
	t.Helper()
	var body map[string]interface{}
	require.NoError(t, json.Unmarshal(r.body, &body))
	return body
}

func bedrockConfig(meteringURL string) *Config {
	return &Config{
		AnthropicAPIKey:    "sk-ant-test",
		AWSAccessKeyID:     "AKIAEXAMPLE",
		AWSSecretAccessKey: "secret",
		AWSRegion:          "us-east-1",
		Revenium: &core.ReveniumConfig{
			APIKey:  "hak_test_key_123",
			BaseURL: meteringURL,
		},
	}
}

func newBedrockAnthropic(t *testing.T, cfg *Config) *ReveniumAnthropic {
	t.Helper()
	cfg.bedrockHTTPClient = newStubBedrockHTTPClient(t)
	r, err := NewReveniumAnthropic(cfg)
	require.NoError(t, err)
	require.Equal(t, ProviderBedrock, r.GetProvider())
	t.Cleanup(func() { _ = r.Close() })
	return r
}

// The AWS SDK closes a request body as soon as the response headers arrive, and the closed
// body's WriteTo returns io.EOF. When a stub answers before net/http's write loop has finished
// its trailing read of that body, the loop takes the io.EOF for a write error and closes the
// connection under a response stream that is still being read. Hiding WriteTo turns that
// trailing read into a plain Read, which reports a closed body as a clean end of input.
type requestBodyWithoutWriteTo struct {
	next http.RoundTripper
}

func (rt requestBodyWithoutWriteTo) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Body == nil || req.Body == http.NoBody {
		return rt.next.RoundTrip(req)
	}
	sent := req.Clone(req.Context())
	sent.Body = struct{ io.ReadCloser }{req.Body}
	return rt.next.RoundTrip(sent)
}

func newStubBedrockHTTPClient(t *testing.T) *http.Client {
	t.Helper()
	transport := http.DefaultTransport.(*http.Transport).Clone()
	t.Cleanup(transport.CloseIdleConnections)
	return &http.Client{Transport: requestBodyWithoutWriteTo{next: transport}}
}

func newMeteringMock(t *testing.T) *testutil.MockMeteringServer {
	t.Helper()
	mock := testutil.NewMockMeteringServer()
	t.Cleanup(mock.Close)
	return mock
}
