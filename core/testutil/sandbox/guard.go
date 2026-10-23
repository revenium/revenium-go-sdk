package sandbox

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"sort"
	"strings"
	"sync"

	"github.com/revenium/revenium-go-sdk/core/internal/netdial"
)

const remedy = "tests may only reach local test servers; point the client at an httptest server"

var errBlocked = errors.New("sandbox: " + remedy)

const resolvConfPath = "/etc/resolv.conf"

type guard struct {
	out           io.Writer
	searchDomains []string
	mu            sync.Mutex
	seen          map[string]bool
}

func newGuard(out io.Writer) *guard {
	return &guard{out: out, searchDomains: resolverSearchDomains(), seen: map[string]bool{}}
}

// resolverSearchDomains reads, once and up front, the search domains the Go
// resolver appends: the resolv.conf entries or, when it names none, the
// domain part of the machine's host name ("local" on a Mac).
func resolverSearchDomains() []string {
	resolvConf, _ := os.ReadFile(resolvConfPath)
	if domains := searchDomains(string(resolvConf)); len(domains) > 0 {
		return domains
	}
	hostname, _ := os.Hostname()
	if _, domain, found := strings.Cut(strings.TrimSuffix(hostname, "."), "."); found && domain != "" {
		return []string{strings.ToLower(domain)}
	}
	return nil
}

func (g *guard) record(attempt string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.seen[attempt] {
		return
	}
	g.seen[attempt] = true
	fmt.Fprintf(g.out, "sandbox: blocked %s: %s\n", attempt, remedy)
}

func (g *guard) attempts() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	list := make([]string, 0, len(g.seen))
	for attempt := range g.seen {
		list = append(list, attempt)
	}
	sort.Strings(list)
	return list
}

type proxyFunc = netdial.ProxyFunc

// proxy wraps the default transport's proxy lookup because the transport
// consults it for every request, on the caller's goroutine, before it dials
// anything. A reserved name is refused at once, so it never reaches a proxy.
func (g *guard) proxy(next proxyFunc) proxyFunc {
	return g.checkedProxy(next, isLocal)
}

// coreProxy wraps the proxy lookup of the transports core builds. A request
// they send through a proxy dials only the proxy, which the dial guard lets
// through when it is local, so the destination is checked here instead. A
// reserved name goes on to next: core's proxy tests send to one through a
// local proxy stub, and it reaches no real server whichever way it goes.
func (g *guard) coreProxy(next proxyFunc) proxyFunc {
	return g.checkedProxy(next, func(host string) bool { return isLocal(host) || isReserved(host) })
}

func (g *guard) checkedProxy(next proxyFunc, allowed func(host string) bool) proxyFunc {
	return func(req *http.Request) (*url.URL, error) {
		host := req.URL.Hostname()
		if allowed(host) {
			if next == nil {
				return nil, nil
			}
			return next(req)
		}
		if !isReserved(host) {
			g.record(fmt.Sprintf("a request to %s%s", redact(req.URL), callingTest()))
		}
		return nil, fmt.Errorf("%w (request to %s)", errBlocked, host)
	}
}

// resolver catches the clients that build their own transport, which never
// consult http.DefaultTransport but still resolve the host name through
// net.DefaultResolver before they connect.
func (g *guard) resolver() *net.Resolver {
	return &net.Resolver{
		PreferGo: true,
		Dial: func(context.Context, string, string) (net.Conn, error) {
			return &blockedLookup{onQuery: g.lookup}, nil
		},
	}
}

func (g *guard) lookup(name string) {
	if name == "" || isHarmlessLookup(name, g.searchDomains) {
		return
	}
	g.record(fmt.Sprintf("a DNS lookup of %s (the resolver cannot tell which test asked; look at the test running when this line was printed)", strings.TrimSuffix(name, ".")))
}

func redact(u *url.URL) string {
	return u.Scheme + "://" + u.Host + u.Path
}

func callingTest() string {
	pcs := make([]uintptr, 64)
	frames := runtime.CallersFrames(pcs[:runtime.Callers(3, pcs)])
	for {
		frame, more := frames.Next()
		if name := testName(frame.Function); name != "" {
			return " from " + name
		}
		if !more {
			return " (made outside the test goroutine; look at the test running when this line was printed)"
		}
	}
}

func testName(function string) string {
	parts := strings.Split(function[strings.LastIndex(function, "/")+1:], ".")
	if len(parts) < 2 || !strings.HasPrefix(parts[1], "Test") || parts[1] == "TestMain" {
		return ""
	}
	return parts[1]
}
