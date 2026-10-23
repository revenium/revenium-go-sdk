package jobs

import (
	"encoding/json"
	"net/http"
	"os"
	"testing"

	"github.com/revenium/revenium-go-sdk/core/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const childBaseURLVar = "REVENIUM_TEST_JOBS_BASE_URL"

func TestJobRequestIsDeliveredThroughHTTPSProxy(t *testing.T) {
	proxy := testutil.NewProxyStub()
	defer proxy.Close()

	testutil.RunTestInChildProcess(t, "TestJobRequestDeliveredChild", proxy.ChildEnv(map[string]string{
		"HTTPS_PROXY":   proxy.URL(),
		childBaseURLVar: "https://revenium.invalid",
	}))

	assert.Equal(t, []string{"CONNECT revenium.invalid:443"}, proxy.Targets())
	assert.Equal(t, []string{"GET " + jobsBasePath}, proxy.Delivered())
}

func TestJobRequestSkipsProxyForNoProxyHost(t *testing.T) {
	proxy := testutil.NewProxyStub()
	defer proxy.Close()

	testutil.RunTestInChildProcess(t, "TestJobRequestNotDeliveredChild", proxy.ChildEnv(map[string]string{
		"HTTPS_PROXY":   proxy.URL(),
		"NO_PROXY":      ".invalid",
		childBaseURLVar: "https://revenium.invalid",
	}))

	assert.Empty(t, proxy.Targets())
	assert.Empty(t, proxy.Delivered())
}

func listJobsFromChild(t *testing.T) error {
	t.Helper()
	testutil.TrustProxyDestination(t, sharedHTTPClient)
	client, err := NewJobClient(JobClientConfig{APIKey: "rev_sk_test_key", BaseURL: os.Getenv(childBaseURLVar), TeamID: "team-1"})
	require.NoError(t, err)
	var raw json.RawMessage
	return client.doRequest(http.MethodGet, jobsBasePath, nil, &raw)
}

func TestJobRequestDeliveredChild(t *testing.T) {
	if !testutil.IsChildProcess() {
		t.Skip("runs in a child process with proxy variables set")
	}
	require.NoError(t, listJobsFromChild(t))
}

func TestJobRequestNotDeliveredChild(t *testing.T) {
	if !testutil.IsChildProcess() {
		t.Skip("runs in a child process with proxy variables set")
	}
	require.Error(t, listJobsFromChild(t))
}
