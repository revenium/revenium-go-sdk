package core_test

import (
	"net/http"
	"net/url"
	"os"
	"testing"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/revenium/revenium-go-sdk/core/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const proxyForChildTest = "http://127.0.0.1:3128"

func resolvedProxy(t *testing.T, target string) *url.URL {
	t.Helper()
	transport, ok := core.NewReveniumHTTPClient(0).Transport.(*http.Transport)
	require.True(t, ok)
	req, err := http.NewRequest(http.MethodPost, target, nil)
	require.NoError(t, err)
	proxyURL, err := transport.Proxy(req)
	require.NoError(t, err)
	return proxyURL
}

func TestReveniumHTTPClient_UsesHTTPSProxy(t *testing.T) {
	testutil.RunTestInChildProcess(t, "TestReveniumHTTPClient_UsesHTTPSProxyChild", map[string]string{
		"HTTPS_PROXY": proxyForChildTest,
	})
}

func TestReveniumHTTPClient_UsesHTTPSProxyChild(t *testing.T) {
	if !testutil.IsChildProcess() {
		t.Skip("runs in a child process with HTTPS_PROXY set")
	}
	proxyURL := resolvedProxy(t, "https://metering.invalid/meter/v2/ai/completions")
	require.NotNil(t, proxyURL)
	assert.Equal(t, proxyForChildTest, proxyURL.String())
}

func TestReveniumHTTPClient_HonoursNoProxy(t *testing.T) {
	testutil.RunTestInChildProcess(t, "TestReveniumHTTPClient_HonoursNoProxyChild", map[string]string{
		"HTTPS_PROXY": proxyForChildTest,
		"NO_PROXY":    "metering.invalid",
	})
}

func TestReveniumHTTPClient_HonoursNoProxyChild(t *testing.T) {
	if !testutil.IsChildProcess() {
		t.Skip("runs in a child process with HTTPS_PROXY and NO_PROXY set")
	}
	assert.Nil(t, resolvedProxy(t, "https://metering.invalid/meter/v2/ai/completions"))
	assert.NotNil(t, resolvedProxy(t, "https://metering.example/meter/v2/ai/completions"))
}

func TestReveniumHTTPClient_ConnectsDirectlyWithoutProxyVariables(t *testing.T) {
	testutil.RunTestInChildProcess(t, "TestReveniumHTTPClient_ConnectsDirectlyWithoutProxyVariablesChild", nil)
}

func TestReveniumHTTPClient_ConnectsDirectlyWithoutProxyVariablesChild(t *testing.T) {
	if !testutil.IsChildProcess() {
		t.Skip("runs in a child process with no proxy variables")
	}
	require.Empty(t, os.Getenv("HTTPS_PROXY"))
	assert.Nil(t, resolvedProxy(t, "https://metering.invalid/meter/v2/ai/completions"))
}

func TestReveniumHTTPClient_CapsConnectionsPerHost(t *testing.T) {
	transport, ok := core.NewReveniumHTTPClient(0).Transport.(*http.Transport)
	require.True(t, ok)

	assert.Equal(t, core.ReveniumMaxConnsPerHost, transport.MaxConnsPerHost)
	assert.Equal(t, core.ReveniumMaxConnsPerHost, transport.MaxIdleConnsPerHost)
}
