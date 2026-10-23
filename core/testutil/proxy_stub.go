package testutil

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
)

const (
	childProcessMarker       = "REVENIUM_TEST_CHILD_PROCESS"
	destinationCertEnv       = "REVENIUM_TEST_PROXY_DESTINATION_CERT"
	destinationCertHostname  = "example.com"
	connectEstablishedStatus = "HTTP/1.1 200 Connection Established\r\n\r\n"
)

// REQUEST_METHOD is not a proxy variable: it is the CGI marker that makes net/http ignore
// HTTP_PROXY entirely (see net/http.ProxyFromEnvironment), so a child inheriting it from a
// CGI or test harness would silently bypass the proxy and the delivery assertion would fail.
var proxyAffectingEnvVars = []string{"HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY", "REQUEST_METHOD"}

// ProxyStub is a forward proxy in front of a stand-in for the Revenium API.
// Every CONNECT tunnel, whatever host it names, is piped to a local TLS
// server; every plain-HTTP forward is answered by the proxy itself. Both
// answer 200 with "{}" and record what reached them, so a test can prove a
// request went through the proxy and arrived.
type ProxyStub struct {
	proxy       *httptest.Server
	destination *httptest.Server

	mu        sync.Mutex
	targets   []string
	delivered []string
}

// NewProxyStub starts a ProxyStub. Caller must invoke Close().
func NewProxyStub() *ProxyStub {
	p := &ProxyStub{}
	p.destination = httptest.NewTLSServer(http.HandlerFunc(p.receive))
	p.proxy = httptest.NewServer(http.HandlerFunc(p.handle))
	return p
}

// URL returns the proxy URL to put in HTTP_PROXY or HTTPS_PROXY.
func (p *ProxyStub) URL() string {
	return p.proxy.URL
}

// Close shuts the proxy and its destination down.
func (p *ProxyStub) Close() {
	p.proxy.Close()
	p.destination.Close()
}

// Targets returns each request the proxy received as "METHOD host" for
// CONNECT tunnels and "METHOD absolute-url" for plain-HTTP forwards.
func (p *ProxyStub) Targets() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.targets...)
}

// Delivered returns each request that arrived through the proxy, tunnelled
// or forwarded, as "METHOD path".
func (p *ProxyStub) Delivered() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.delivered...)
}

// ChildEnv returns the variables a child process needs to trust the
// destination, merged with the proxy variables in env.
func (p *ProxyStub) ChildEnv(env map[string]string) map[string]string {
	merged := map[string]string{destinationCertEnv: p.destinationCertPEM()}
	for key, value := range env {
		merged[key] = value
	}
	return merged
}

// TrustProxyDestination makes client accept the destination of the ProxyStub
// whose ChildEnv started this process, leaving the client's proxy settings as
// they are.
func TrustProxyDestination(t *testing.T, client *http.Client) {
	t.Helper()
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("client transport is %T, want *http.Transport", client.Transport)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM([]byte(os.Getenv(destinationCertEnv))) {
		t.Fatalf("%s does not hold a PEM certificate", destinationCertEnv)
	}
	transport.TLSClientConfig = &tls.Config{RootCAs: roots, ServerName: destinationCertHostname}
}

func (p *ProxyStub) destinationCertPEM() string {
	cert := p.destination.Certificate()
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw}))
}

func (p *ProxyStub) record(list *[]string, entry string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	*list = append(*list, entry)
}

func (p *ProxyStub) receive(w http.ResponseWriter, r *http.Request) {
	p.record(&p.delivered, r.Method+" "+r.URL.Path)
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte("{}"))
}

func (p *ProxyStub) handle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodConnect {
		p.record(&p.targets, r.Method+" "+r.URL.String())
		p.receive(w, r)
		return
	}
	p.record(&p.targets, r.Method+" "+r.Host)
	p.tunnel(w)
}

func (p *ProxyStub) tunnel(w http.ResponseWriter) {
	upstream, err := net.Dial("tcp", p.destination.Listener.Addr().String())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		upstream.Close()
		http.Error(w, "tunnelling not supported", http.StatusInternalServerError)
		return
	}
	client, _, err := hijacker.Hijack()
	if err != nil {
		upstream.Close()
		return
	}
	if _, err := client.Write([]byte(connectEstablishedStatus)); err != nil {
		client.Close()
		upstream.Close()
		return
	}
	go pipe(upstream, client)
	go pipe(client, upstream)
}

func pipe(dst, src net.Conn) {
	defer dst.Close()
	_, _ = io.Copy(dst, src)
}

// IsChildProcess reports whether the running test was started by
// RunTestInChildProcess.
func IsChildProcess() bool {
	return os.Getenv(childProcessMarker) == "1"
}

// RunTestInChildProcess runs the named test of the current test binary in a
// fresh process whose proxy variables are exactly env. net/http reads the
// proxy variables once per process, so proxy behaviour can only be tested
// with the variables in place before the first request is made.
func RunTestInChildProcess(t *testing.T, testName string, env map[string]string) {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^"+testName+"$", "-test.count=1", "-test.v")
	cmd.Env = append(withoutProxyVariables(os.Environ()), childProcessMarker+"=1")
	for key, value := range env {
		cmd.Env = append(cmd.Env, key+"="+value)
	}
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("child test %s failed: %v\n%s", testName, err, output)
	}
	if !strings.Contains(string(output), "--- PASS: "+testName) {
		t.Fatalf("child test %s did not run and pass:\n%s", testName, output)
	}
}

func withoutProxyVariables(environ []string) []string {
	kept := make([]string, 0, len(environ))
	for _, entry := range environ {
		name := strings.SplitN(entry, "=", 2)[0]
		if isProxyVariable(name) {
			continue
		}
		kept = append(kept, entry)
	}
	return kept
}

func isProxyVariable(name string) bool {
	for _, proxyVar := range proxyAffectingEnvVars {
		if strings.EqualFold(name, proxyVar) {
			return true
		}
	}
	return false
}
