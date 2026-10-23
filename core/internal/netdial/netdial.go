// Package netdial picks the proxy for, and opens every connection of, the
// transports core builds, so the test sandbox can refuse requests and
// connections to hosts that are not local at the one place they all pass
// through.
package netdial

import (
	"context"
	"net"
	"net/http"
	"net/url"
)

// DialFunc has the signature of http.Transport.DialContext.
type DialFunc func(ctx context.Context, network, address string) (net.Conn, error)

// ProxyFunc has the signature of http.Transport.Proxy.
type ProxyFunc func(*http.Request) (*url.URL, error)

var (
	current      DialFunc  = (&net.Dialer{}).DialContext
	currentProxy ProxyFunc = http.ProxyFromEnvironment
)

// DialContext opens a connection with the dial function in force. Outside
// tests that is a zero net.Dialer, which is what http.Transport uses when its
// DialContext is unset. Transports look it up on every dial rather than
// keeping a copy, because core builds its shared clients when the package
// loads, before a test's TestMain can install the sandbox.
func DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	return current(ctx, network, address)
}

// Proxy picks the proxy for req with the proxy function in force, which
// outside tests is http.ProxyFromEnvironment. It is looked up on every
// request for the same reason as DialContext. A request sent through a proxy
// dials only the proxy, so the dial function never sees its destination.
func Proxy(req *http.Request) (*url.URL, error) {
	return currentProxy(req)
}

// WrapForTest puts wrap around the dial function for the rest of the
// process. Only core/testutil/sandbox calls it, from TestMain before any test
// runs.
func WrapForTest(wrap func(DialFunc) DialFunc) {
	current = wrap(current)
}

// WrapProxyForTest puts wrap around the proxy function for the rest of the
// process, under the same rules as WrapForTest.
func WrapProxyForTest(wrap func(ProxyFunc) ProxyFunc) {
	currentProxy = wrap(currentProxy)
}
