package google

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/revenium/revenium-go-sdk/core/testutil"
	"github.com/stretchr/testify/require"
	"google.golang.org/genai"
)

type providerStub struct {
	server *httptest.Server
	mu     sync.Mutex
	seen   []string
}

func (s *providerStub) requests() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.seen...)
}

type stubbedClient struct {
	client   *ReveniumGoogle
	provider *providerStub
	revenium *testutil.MockMeteringServer
}

func (c *stubbedClient) records(t *testing.T) []map[string]interface{} {
	t.Helper()
	c.client.Flush()
	return c.revenium.GetPayloads()
}

func newStubbedClient(t *testing.T, provider Provider, handler http.HandlerFunc) *stubbedClient {
	t.Helper()
	for _, name := range []string{"GOOGLE_API_KEY", "GEMINI_API_KEY", "GOOGLE_CLOUD_PROJECT", "GOOGLE_CLOUD_LOCATION", "GOOGLE_CLOUD_REGION", "GOOGLE_GENAI_USE_VERTEXAI", "GOOGLE_GEMINI_BASE_URL", "GOOGLE_VERTEX_BASE_URL"} {
		t.Setenv(name, "")
	}

	stub := &providerStub{}
	stub.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		stub.mu.Lock()
		stub.seen = append(stub.seen, r.Method+" "+r.URL.Path)
		stub.mu.Unlock()
		handler(w, r)
	}))
	t.Cleanup(stub.server.Close)

	revenium := testutil.NewMockMeteringServer()
	t.Cleanup(revenium.Close)

	client := newClientAgainst(t, provider, stub.server.URL, &core.ReveniumConfig{APIKey: "hak_test", BaseURL: revenium.URL()})
	return &stubbedClient{client: client, provider: stub, revenium: revenium}
}

func newClientAgainst(t *testing.T, provider Provider, googleURL string, revenium *core.ReveniumConfig) *ReveniumGoogle {
	t.Helper()
	genaiConfig := &genai.ClientConfig{HTTPOptions: genai.HTTPOptions{BaseURL: googleURL}}
	if provider.IsVertexAI() {
		genaiConfig.Backend = genai.BackendVertexAI
	} else {
		genaiConfig.Backend = genai.BackendGeminiAPI
		genaiConfig.APIKey = "test-google-key"
	}
	genaiClient, err := genai.NewClient(context.Background(), genaiConfig)
	require.NoError(t, err)

	cfg := &Config{Revenium: revenium}
	mc, err := newMeteringClient(cfg)
	require.NoError(t, err)

	client, err := assembleReveniumGoogle(context.Background(), cfg, provider, genaiClient, mc)
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func writeSSE(w http.ResponseWriter, chunks ...string) {
	w.Header().Set("Content-Type", "text/event-stream")
	for _, chunk := range chunks {
		fmt.Fprintf(w, "data: %s\r\n\r\n", chunk)
	}
}

func writeJSON(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(body))
}

func isStreamRequest(r *http.Request) bool {
	return strings.HasSuffix(r.URL.Path, ":streamGenerateContent")
}

func singleRecord(t *testing.T, c *stubbedClient) map[string]interface{} {
	t.Helper()
	records := c.records(t)
	require.Len(t, records, 1)
	return records[0]
}
