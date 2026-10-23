package enforcement

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const documentedRuleFetchTimeout = 10 * time.Second

func TestRuleFetch_UsesTheDocumentedFixedTimeout(t *testing.T) {
	t.Setenv(core.EnvAPITimeout, "45s")

	p := newPoller(engineKey{baseURL: "http://127.0.0.1:9", apiKey: "hak_test", teamID: "team-1"})

	assert.Equal(t, documentedRuleFetchTimeout, p.client.Timeout)
}

func TestRuleFetch_IgnoresTheUsageRecordTimeoutSetting(t *testing.T) {
	t.Setenv(core.EnvAPITimeout, "1ms")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(50 * time.Millisecond)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	p := newPoller(engineKey{baseURL: server.URL, apiKey: "hak_test", teamID: "team-1"})

	require.NoError(t, p.fetchRules(context.Background()))
}
