package metering

import (
	"os"
	"testing"

	"github.com/revenium/revenium-go-sdk/core/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	childBaseURLVar = "REVENIUM_TEST_METERING_BASE_URL"
	completionsPath = "/meter/v2/ai/completions"
)

func TestMeteringRequestIsDeliveredThroughHTTPSProxy(t *testing.T) {
	proxy := testutil.NewProxyStub()
	defer proxy.Close()

	testutil.RunTestInChildProcess(t, "TestMeteringRequestDeliveredChild", proxy.ChildEnv(map[string]string{
		"HTTPS_PROXY":   proxy.URL(),
		childBaseURLVar: "https://revenium.invalid",
	}))

	assert.Equal(t, []string{"CONNECT revenium.invalid:443"}, proxy.Targets())
	assert.Equal(t, []string{"POST " + completionsPath}, proxy.Delivered())
}

func TestMeteringRequestIsDeliveredThroughHTTPProxy(t *testing.T) {
	proxy := testutil.NewProxyStub()
	defer proxy.Close()

	testutil.RunTestInChildProcess(t, "TestMeteringRequestDeliveredChild", proxy.ChildEnv(map[string]string{
		"HTTP_PROXY":    proxy.URL(),
		childBaseURLVar: "http://revenium.invalid",
	}))

	assert.Equal(t, []string{"POST http://revenium.invalid" + completionsPath}, proxy.Targets())
	assert.Equal(t, []string{"POST " + completionsPath}, proxy.Delivered())
}

func TestMeteringRequestSkipsProxyForNoProxyHost(t *testing.T) {
	proxy := testutil.NewProxyStub()
	defer proxy.Close()

	testutil.RunTestInChildProcess(t, "TestMeteringRequestNotDeliveredChild", proxy.ChildEnv(map[string]string{
		"HTTPS_PROXY":   proxy.URL(),
		"NO_PROXY":      "revenium.invalid",
		childBaseURLVar: "https://revenium.invalid",
	}))

	assert.Empty(t, proxy.Targets())
	assert.Empty(t, proxy.Delivered())
}

func sendFromChild(t *testing.T) error {
	t.Helper()
	testutil.TrustProxyDestination(t, sharedHTTPClient)
	client, err := NewMeteringClient(MeteringClientConfig{APIKey: "test-key", BaseURL: os.Getenv(childBaseURLVar)})
	require.NoError(t, err)
	record, err := client.encodeUsageRecord(NewPayload(OperationChat, "gpt-4", "OPENAI").Build())
	require.NoError(t, err)
	return client.path.sender.post(record.URL, record.Body, record.Headers, usageRecordKind.label)
}

func TestMeteringRequestDeliveredChild(t *testing.T) {
	if !testutil.IsChildProcess() {
		t.Skip("runs in a child process with proxy variables set")
	}
	require.NoError(t, sendFromChild(t))
}

func TestMeteringRequestNotDeliveredChild(t *testing.T) {
	if !testutil.IsChildProcess() {
		t.Skip("runs in a child process with proxy variables set")
	}
	require.Error(t, sendFromChild(t))
}
