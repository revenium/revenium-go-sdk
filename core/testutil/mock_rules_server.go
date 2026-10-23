package testutil

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"

	"github.com/revenium/revenium-go-sdk/core"
)

// MockRulesServer serves enforcement-rule payloads per team and counts how
// often each team's rules are fetched. A team with no payload gets 204.
type MockRulesServer struct {
	server   *httptest.Server
	mu       sync.Mutex
	payloads map[string]string
	fetches  map[string]int
}

func NewMockRulesServer() *MockRulesServer {
	m := &MockRulesServer{payloads: map[string]string{}, fetches: map[string]int{}}
	m.server = httptest.NewServer(http.HandlerFunc(m.handle))
	return m
}

func (m *MockRulesServer) URL() string {
	return m.server.URL
}

func (m *MockRulesServer) Close() {
	m.server.Close()
}

// SetPayload sets the JSON body served for teamID.
func (m *MockRulesServer) SetPayload(teamID, body string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.payloads[teamID] = body
}

// Fetches returns how many times teamID's rules were requested.
func (m *MockRulesServer) Fetches(teamID string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.fetches[teamID]
}

func (m *MockRulesServer) handle(w http.ResponseWriter, r *http.Request) {
	teamID, ok := strings.CutPrefix(r.URL.Path, core.EnforcementRulesPathPrefix)
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	m.mu.Lock()
	m.fetches[teamID]++
	body, found := m.payloads[teamID]
	m.mu.Unlock()
	if !found {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(body))
}
