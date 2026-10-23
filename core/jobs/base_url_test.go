package jobs

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func outcomeServer(t *testing.T) (*httptest.Server, *[]string) {
	t.Helper()
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		_, _ = w.Write([]byte(`{"id":"j1","agenticJobId":"job-1","hasOutcome":true}`))
	}))
	t.Cleanup(srv.Close)
	return srv, &paths
}

func TestJobClient_OutcomesGoToTheMeteringBaseURLSetting(t *testing.T) {
	srv, paths := outcomeServer(t)
	t.Setenv(core.EnvBaseURL, srv.URL)
	client, err := NewJobClient(JobClientConfig{APIKey: "rev_sk_test", TeamID: "team-1"})
	require.NoError(t, err)
	t.Cleanup(client.Close)

	_, err = client.ReportJobOutcome("job-1", &JobOutcome{ExecutionStatus: ExecutionStatusSuccess})

	require.NoError(t, err)
	assert.Equal(t, []string{"/profitstream/v2/api/jobs/job-1/outcome"}, *paths)
}

func TestJobClient_ExplicitBaseURLWinsOverTheSetting(t *testing.T) {
	explicit, explicitPaths := outcomeServer(t)
	fromEnv, envPaths := outcomeServer(t)
	t.Setenv(core.EnvBaseURL, fromEnv.URL)
	client, err := NewJobClient(JobClientConfig{APIKey: "rev_sk_test", TeamID: "team-1", BaseURL: explicit.URL + "/"})
	require.NoError(t, err)
	t.Cleanup(client.Close)

	_, err = client.ReportJobOutcome("job-1", &JobOutcome{ExecutionStatus: ExecutionStatusSuccess})

	require.NoError(t, err)
	assert.Equal(t, []string{"/profitstream/v2/api/jobs/job-1/outcome"}, *explicitPaths)
	assert.Empty(t, *envPaths)
}

func TestNewJobClient_NamesTheSettingThatHeldAnInvalidBaseURL(t *testing.T) {
	t.Run("config", func(t *testing.T) {
		t.Setenv(core.EnvBaseURL, "")
		_, err := NewJobClient(JobClientConfig{APIKey: "rev_sk_test", TeamID: "team-1", BaseURL: "api.dev.example.test"})

		require.Error(t, err)
		assert.True(t, core.IsConfigError(err))
		assert.Contains(t, err.Error(), "JobClientConfig.BaseURL")
		assert.NotContains(t, err.Error(), core.EnvBaseURL)
	})
	t.Run("environment", func(t *testing.T) {
		t.Setenv(core.EnvBaseURL, "api.dev.example.test")
		_, err := NewJobClient(JobClientConfig{APIKey: "rev_sk_test", TeamID: "team-1"})

		require.Error(t, err)
		assert.Contains(t, err.Error(), core.EnvBaseURL)
	})
}
