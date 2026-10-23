package sandbox

import (
	"bytes"
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/revenium/revenium-go-sdk/core/metering"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	childMarker      = "REVENIUM_SANDBOX_CHILD"
	childBaseURLVar  = "REVENIUM_SANDBOX_CHILD_BASE_URL"
	neverRoutable    = "203.0.113.9:80"
	neverRoutableURL = "http://" + neverRoutable
	otherUnroutable  = "198.51.100.7:80"
)

func refuseToDial(t *testing.T) func(context.Context, string, string) (net.Conn, error) {
	return func(context.Context, string, string) (net.Conn, error) {
		t.Error("the sandbox let a connection to a host that is not local through")
		return nil, errBlocked
	}
}

func TestDialBlocksAndRecordsAConnectionToANonLoopbackIPLiteral(t *testing.T) {
	g := newGuard(&bytes.Buffer{})

	_, err := g.dial(refuseToDial(t))(context.Background(), "tcp", neverRoutable)

	require.ErrorIs(t, err, errBlocked)
	require.Len(t, g.attempts(), 1)
	assert.Contains(t, g.attempts()[0], "a connection to "+neverRoutable)
}

func TestDialLetsLoopbackConnectionsThrough(t *testing.T) {
	g := newGuard(&bytes.Buffer{})
	var dialed []string
	next := func(_ context.Context, _, address string) (net.Conn, error) {
		dialed = append(dialed, address)
		return nil, nil
	}

	for _, address := range []string{"127.0.0.1:8080", "[::1]:8080", "localhost:8080"} {
		_, err := g.dial(next)(context.Background(), "tcp", address)
		require.NoError(t, err, address)
	}

	assert.Equal(t, []string{"127.0.0.1:8080", "[::1]:8080", "localhost:8080"}, dialed)
	assert.Empty(t, g.attempts())
}

func TestRunClearsAShellProxyThatIsNotLocal(t *testing.T) {
	t.Setenv("HTTPS_PROXY", neverRoutableURL)
	t.Setenv("http_proxy", "203.0.113.9:3128")
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:3128")

	clearRemoteProxies()

	_, httpsSet := os.LookupEnv("HTTPS_PROXY")
	_, lowerSet := os.LookupEnv("http_proxy")
	assert.False(t, httpsSet)
	assert.False(t, lowerSet)
	assert.Equal(t, "http://127.0.0.1:3128", os.Getenv("HTTP_PROXY"))
}

func TestMeteringClientSendingToANonLoopbackIPFailsThePackage(t *testing.T) {
	output := runFailingChild(t, map[string]string{childBaseURLVar: neverRoutableURL})

	assert.Contains(t, output, "sandbox: blocked a request to "+neverRoutableURL)
}

func TestMeteringClientIgnoresAShellProxyThatIsNotLocal(t *testing.T) {
	output := runFailingChild(t, map[string]string{
		childBaseURLVar: "http://" + otherUnroutable,
		"HTTP_PROXY":    neverRoutableURL,
		"HTTPS_PROXY":   neverRoutableURL,
	})

	assert.Contains(t, output, "sandbox: blocked a request to http://"+otherUnroutable)
	assert.NotContains(t, output, neverRoutable)
}

const productionURL = "https://api.revenium.ai"

// recordingProxy stands in for a developer's local forwarding proxy: it
// records the destination of every request it receives and refuses it, so a
// test can see whether anything was handed to it.
func recordingProxy(t *testing.T) (*httptest.Server, func() []string) {
	t.Helper()
	var mu sync.Mutex
	var received []string
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		received = append(received, r.Method+" "+r.Host)
		mu.Unlock()
		w.WriteHeader(http.StatusForbidden)
	}))
	t.Cleanup(proxy.Close)
	return proxy, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), received...)
	}
}

func TestMeteringClientSendingToProductionThroughALocalForwardingProxyFailsThePackage(t *testing.T) {
	proxy, received := recordingProxy(t)

	output := runFailingChild(t, map[string]string{
		childBaseURLVar: productionURL,
		"HTTPS_PROXY":   proxy.URL,
		"HTTP_PROXY":    proxy.URL,
	})

	assert.Contains(t, output, "sandbox: blocked a request to "+productionURL)
	assert.Empty(t, received(), "the request never reaches the local proxy")
}

func TestMeteringClientSendingToProductionThroughAnUnreachableLocalProxyFailsThePackage(t *testing.T) {
	output := runFailingChild(t, map[string]string{
		childBaseURLVar: productionURL,
		"HTTPS_PROXY":   "http://127.0.0.1:9",
	})

	assert.Contains(t, output, "sandbox: blocked a request to "+productionURL)
}

func TestCoreProxyLetsLocalAndReservedHostsReachTheConfiguredProxy(t *testing.T) {
	g := newGuard(&bytes.Buffer{})
	local, err := url.Parse("http://127.0.0.1:3128")
	require.NoError(t, err)
	next := func(*http.Request) (*url.URL, error) { return local, nil }

	for _, target := range []string{"https://revenium.invalid/meter", "http://127.0.0.1:8080/x", "https://svc.test/x"} {
		req, err := http.NewRequest(http.MethodPost, target, nil)
		require.NoError(t, err)

		proxyURL, err := g.coreProxy(next)(req)

		require.NoError(t, err, target)
		assert.Equal(t, local, proxyURL, target)
	}
	assert.Empty(t, g.attempts())
}

func TestCoreProxyRefusesARealHostBeforeChoosingAProxy(t *testing.T) {
	g := newGuard(&bytes.Buffer{})
	next := func(*http.Request) (*url.URL, error) {
		t.Error("the proxy was chosen for a real host")
		return nil, nil
	}
	req, err := http.NewRequest(http.MethodPost, productionURL+"/meter/v2/ai/completions", nil)
	require.NoError(t, err)

	_, err = g.coreProxy(next)(req)

	require.ErrorIs(t, err, errBlocked)
	require.Len(t, g.attempts(), 1)
	assert.Contains(t, g.attempts()[0], "a request to "+productionURL)
}

func TestMeteringClientSendChild(t *testing.T) {
	if os.Getenv(childMarker) == "" {
		t.Skip("runs in a child process started by a test in this file")
	}
	client, err := metering.NewMeteringClient(metering.MeteringClientConfig{
		APIKey:         "test-key",
		BaseURL:        os.Getenv(childBaseURLVar),
		RequestTimeout: time.Second,
	})
	require.NoError(t, err)
	defer client.Close()

	start := time.Now()
	err = client.SendSync(metering.NewPayload(metering.OperationChat, "gpt-4", "OPENAI").Build())

	require.ErrorContains(t, err, "sandbox:")
	assert.Less(t, time.Since(start), 30*time.Second)
}

func runFailingChild(t *testing.T, env map[string]string) string {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestMeteringClientSendChild$", "-test.count=1", "-test.v")
	cmd.Env = append(withoutVariables(os.Environ(), proxyVariables), childMarker+"=1")
	for key, value := range env {
		cmd.Env = append(cmd.Env, key+"="+value)
	}
	output, err := cmd.CombinedOutput()

	var exitErr *exec.ExitError
	require.ErrorAs(t, err, &exitErr, "the child package passed although it reached a host that is not local:\n%s", output)
	require.Contains(t, string(output), "--- PASS: TestMeteringClientSendChild", "the child test itself must pass:\n%s", output)
	return string(output)
}

func withoutVariables(environ, names []string) []string {
	kept := make([]string, 0, len(environ))
	for _, entry := range environ {
		if !isOneOf(entry, names) {
			kept = append(kept, entry)
		}
	}
	return kept
}

func isOneOf(entry string, names []string) bool {
	for _, name := range names {
		if strings.HasPrefix(entry, name+"=") {
			return true
		}
	}
	return false
}
