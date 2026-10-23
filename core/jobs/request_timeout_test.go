package jobs

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const documentedJobRequestTimeout = 10 * time.Second

func TestJobRequests_UseTheDocumentedFixedTimeout(t *testing.T) {
	t.Setenv(core.EnvAPITimeout, "45s")

	assert.Equal(t, documentedJobRequestTimeout, newJobHTTPClient().Timeout)
}

func TestJobRequests_IgnoreTheUsageRecordTimeoutSetting(t *testing.T) {
	t.Setenv(core.EnvAPITimeout, "1ms")
	original := sharedHTTPClient
	sharedHTTPClient = newJobHTTPClient()
	t.Cleanup(func() { sharedHTTPClient = original })
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(50 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(JobResource{ID: "abc", AgenticJobID: "slow-job"})
	}))
	defer server.Close()

	result, err := newTestClient(t, server.URL).GetJob("slow-job")

	require.NoError(t, err)
	assert.Equal(t, "slow-job", result.AgenticJobID)
}
