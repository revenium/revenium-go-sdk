// Package sandbox keeps a test binary from reaching any server other than
// the local test servers it starts. Every test package calls Run from its
// TestMain.
package sandbox

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/revenium/revenium-go-sdk/core/internal/netdial"
)

// UnreachableAddress is the Revenium address every test starts with: a
// local port nothing listens on, so a request a test forgot to point at its
// own server fails at once instead of leaving the machine.
const UnreachableAddress = "http://127.0.0.1:9"

var reveniumAddressVariables = []string{
	"REVENIUM_METERING_BASE_URL",
	"REVENIUM_BASE_URL",
}

var proxyVariables = []string{"HTTP_PROXY", "HTTPS_PROXY", "http_proxy", "https_proxy"}

// The cost-control address is cleared rather than set because, when it is
// unset, the SDK reads rules from the metering address, which already points
// at UnreachableAddress; tests of that fallback need it unset.
var clearedVariables = []string{
	"REVENIUM_ENFORCEMENT_BASE_URL",
	"REVENIUM_METERING_API_KEY",
	"REVENIUM_ENFORCEMENT_API_KEY",
	"REVENIUM_API_KEY",
	"REVENIUM_WRITE_API_KEY",
	"REVENIUM_TEAM_ID",
}

// Run points every Revenium address at UnreachableAddress, clears the
// Revenium credentials, cost-control address and any proxy off the machine
// inherited from the shell, blocks every connection to a host that is not
// local, runs the tests and returns the exit code for os.Exit.
// A blocked attempt to reach a real host fails the run even when every test
// passed, because code that sends in the background never fails a test.
func Run(m *testing.M) int {
	isolateEnvironment()
	g := install()
	code := m.Run()
	if g.report(os.Stderr) && code == 0 {
		return 1
	}
	return code
}

func isolateEnvironment() {
	for _, name := range reveniumAddressVariables {
		_ = os.Setenv(name, UnreachableAddress)
	}
	for _, name := range clearedVariables {
		_ = os.Unsetenv(name)
	}
	clearRemoteProxies()
}

// clearRemoteProxies drops a shell proxy that is not on this machine, so no
// client that honours the proxy variables sends through it. A loopback proxy
// stays: it is CI's egress sink or a test's own proxy stub. The proxy guards
// installed by install refuse a request to a real host before any proxy is
// chosen only for http.DefaultTransport and the transports core builds. A
// client with a transport of its own, such as the AWS SDK client used for
// Bedrock, still sends through a loopback proxy, and only CI's egress sink
// keeps that request on the machine.
func clearRemoteProxies() {
	for _, name := range proxyVariables {
		if value, set := os.LookupEnv(name); set && !isLocalProxy(value) {
			_ = os.Unsetenv(name)
		}
	}
}

func isLocalProxy(value string) bool {
	if !strings.Contains(value, "://") {
		value = "http://" + value
	}
	u, err := url.Parse(value)
	return err == nil && isLocal(u.Hostname())
}

func install() *guard {
	g := newGuard(os.Stderr)
	if transport, ok := http.DefaultTransport.(*http.Transport); ok {
		transport.Proxy = g.proxy(transport.Proxy)
		transport.DialContext = g.dial(transport.DialContext)
	}
	netdial.WrapForTest(g.dial)
	netdial.WrapProxyForTest(g.coreProxy)
	net.DefaultResolver = g.resolver()
	return g
}

func (g *guard) report(w io.Writer) bool {
	attempts := g.attempts()
	if len(attempts) == 0 {
		return false
	}
	fmt.Fprintf(w, "\nsandbox: the tests made %d blocked attempt(s) to reach hosts that are not local test servers:\n", len(attempts))
	for _, attempt := range attempts {
		fmt.Fprintf(w, "  - %s\n", attempt)
	}
	return true
}
