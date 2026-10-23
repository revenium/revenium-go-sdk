package enforcement

import (
	"context"
	"os"
	"testing"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/revenium/revenium-go-sdk/core/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const childRulesBaseURLVar = "REVENIUM_TEST_RULES_BASE_URL"

var rulesPath = core.EnforcementRulesPathPrefix + testTeamID

func TestRuleFetchIsDeliveredThroughHTTPSProxy(t *testing.T) {
	proxy := testutil.NewProxyStub()
	defer proxy.Close()

	testutil.RunTestInChildProcess(t, "TestRuleFetchDeliveredChild", proxy.ChildEnv(map[string]string{
		"HTTPS_PROXY":        proxy.URL(),
		childRulesBaseURLVar: "https://revenium.invalid",
	}))

	assert.Equal(t, []string{"CONNECT revenium.invalid:443"}, proxy.Targets())
	assert.Equal(t, []string{"GET " + rulesPath}, proxy.Delivered())
}

func TestRuleFetchIsDeliveredThroughHTTPProxy(t *testing.T) {
	proxy := testutil.NewProxyStub()
	defer proxy.Close()

	testutil.RunTestInChildProcess(t, "TestRuleFetchDeliveredChild", proxy.ChildEnv(map[string]string{
		"HTTP_PROXY":         proxy.URL(),
		childRulesBaseURLVar: "http://revenium.invalid",
	}))

	assert.Equal(t, []string{"GET http://revenium.invalid" + rulesPath}, proxy.Targets())
	assert.Equal(t, []string{"GET " + rulesPath}, proxy.Delivered())
}

func TestRuleFetchSkipsProxyForNoProxyHost(t *testing.T) {
	proxy := testutil.NewProxyStub()
	defer proxy.Close()

	testutil.RunTestInChildProcess(t, "TestRuleFetchNotDeliveredChild", proxy.ChildEnv(map[string]string{
		"HTTPS_PROXY":        proxy.URL(),
		"NO_PROXY":           "revenium.invalid",
		childRulesBaseURLVar: "https://revenium.invalid",
	}))

	assert.Empty(t, proxy.Targets())
	assert.Empty(t, proxy.Delivered())
}

func fetchFromChild(t *testing.T) error {
	t.Helper()
	p := newPoller(engineKey{baseURL: os.Getenv(childRulesBaseURLVar), apiKey: "test-key", teamID: testTeamID})
	testutil.TrustProxyDestination(t, p.client)
	return p.fetchRules(context.Background())
}

func TestRuleFetchDeliveredChild(t *testing.T) {
	if !testutil.IsChildProcess() {
		t.Skip("runs in a child process with proxy variables set")
	}
	assert.ErrorContains(t, fetchFromChild(t), "decode rules", "the stand-in answers {} once the request arrives")
}

func TestRuleFetchNotDeliveredChild(t *testing.T) {
	if !testutil.IsChildProcess() {
		t.Skip("runs in a child process with proxy variables set")
	}
	require.ErrorContains(t, fetchFromChild(t), "http get")
}
