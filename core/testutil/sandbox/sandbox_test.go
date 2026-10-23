package sandbox

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	os.Exit(Run(m))
}

func TestRunPointsEveryReveniumAddressAtTheUnreachableAddress(t *testing.T) {
	for _, name := range reveniumAddressVariables {
		assert.Equal(t, UnreachableAddress, os.Getenv(name), name)
	}
}

func TestRunClearsReveniumCredentialsAndTheCostControlAddressFromTheShell(t *testing.T) {
	for _, name := range clearedVariables {
		_, set := os.LookupEnv(name)
		assert.False(t, set, name)
	}
}

func TestDefaultClientStillReachesLocalTestServers(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	defer server.Close()

	resp, err := http.Get(server.URL)
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestProxyRecordsARequestToARealHostWithTheTestThatMadeIt(t *testing.T) {
	var out bytes.Buffer
	g := newGuard(&out)
	req, err := http.NewRequest(http.MethodPost, "https://api.revenium.ai/meter/v2/ai/completions?key=secret", nil)
	require.NoError(t, err)

	proxyURL, err := g.proxy(http.ProxyFromEnvironment)(req)

	assert.Nil(t, proxyURL)
	require.ErrorIs(t, err, errBlocked)
	assert.Equal(t, []string{"a request to https://api.revenium.ai/meter/v2/ai/completions from TestProxyRecordsARequestToARealHostWithTheTestThatMadeIt"}, g.attempts())
	assert.Contains(t, out.String(), "sandbox: blocked a request to https://api.revenium.ai")
	assert.NotContains(t, out.String(), "secret")
}

func TestProxyBlocksReservedHostsWithoutRecordingThem(t *testing.T) {
	g := newGuard(&bytes.Buffer{})
	for _, target := range []string{"https://revenium.invalid/x", "https://api.revenium.example/x", "https://example.com/x", "https://svc.test/x"} {
		req, err := http.NewRequest(http.MethodGet, target, nil)
		require.NoError(t, err)

		_, err = g.proxy(nil)(req)

		require.ErrorIs(t, err, errBlocked, target)
	}
	assert.Empty(t, g.attempts())
}

func TestProxyDefersToTheTransportProxyForLocalHosts(t *testing.T) {
	g := newGuard(&bytes.Buffer{})
	local, err := url.Parse("http://127.0.0.1:3128")
	require.NoError(t, err)
	next := func(*http.Request) (*url.URL, error) { return local, nil }

	for _, target := range []string{"http://127.0.0.1:8080/x", "http://localhost/x", "http://[::1]:80/x"} {
		req, err := http.NewRequest(http.MethodGet, target, nil)
		require.NoError(t, err)

		proxyURL, err := g.proxy(next)(req)

		require.NoError(t, err, target)
		assert.Equal(t, local, proxyURL, target)
	}
	assert.Empty(t, g.attempts())
}

func TestResolverBlocksAndRecordsALookupOfARealHost(t *testing.T) {
	g := newGuard(&bytes.Buffer{})

	_, err := g.resolver().LookupHost(context.Background(), "api.revenium.ai")

	require.Error(t, err)
	require.NotEmpty(t, g.attempts())
	assert.Contains(t, g.attempts()[0], "a DNS lookup of api.revenium.ai")
}

func TestResolverBlocksReservedNamesWithoutRecordingThem(t *testing.T) {
	g := newGuard(&bytes.Buffer{})

	_, err := g.resolver().LookupHost(context.Background(), "revenium.invalid")

	require.Error(t, err)
	assert.Empty(t, g.attempts())
}

func TestHarmlessLookupAcceptsReservedNamesFollowedByASearchDomain(t *testing.T) {
	search := []string{"lan", "corp.internal"}

	assert.True(t, isHarmlessLookup("revenium.invalid.", search))
	assert.True(t, isHarmlessLookup("revenium.invalid.lan.", search))
	assert.True(t, isHarmlessLookup("localhost.corp.internal.", search))
	assert.False(t, isHarmlessLookup("revenium.invalid.lan.", nil))
	assert.False(t, isHarmlessLookup("api.revenium.ai.lan.", search))
	assert.False(t, isHarmlessLookup("api.revenium.ai.", search))
}

func TestHarmlessLookupRejectsARealHostWhoseFirstLabelIsReserved(t *testing.T) {
	search := []string{"lan"}

	assert.False(t, isHarmlessLookup("test.revenium.ai.", search))
	assert.False(t, isHarmlessLookup("example.openai.azure.com.", search))
	assert.False(t, isHarmlessLookup("localhost.corp.net.", search))
}

func TestSearchDomainsReadsSearchAndDomainEntries(t *testing.T) {
	resolvConf := "# comment\nnameserver 10.0.0.2\ndomain Corp.Internal.\nsearch lan svc.cluster.local\n"

	assert.Equal(t, []string{"corp.internal", "lan", "svc.cluster.local"}, searchDomains(resolvConf))
}

func TestReportListsEachAttemptOnce(t *testing.T) {
	var out bytes.Buffer
	g := newGuard(&bytes.Buffer{})
	g.record("a DNS lookup of api.revenium.ai")
	g.record("a DNS lookup of api.revenium.ai")

	assert.True(t, g.report(&out))
	assert.Equal(t, "\nsandbox: the tests made 1 blocked attempt(s) to reach hosts that are not local test servers:\n  - a DNS lookup of api.revenium.ai\n", out.String())
}

func TestReportIsSilentWhenNothingWasBlocked(t *testing.T) {
	var out bytes.Buffer

	assert.False(t, newGuard(&bytes.Buffer{}).report(&out))
	assert.Empty(t, out.String())
}
