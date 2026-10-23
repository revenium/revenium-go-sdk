package core

import (
	"net/http"
	"time"

	"github.com/revenium/revenium-go-sdk/core/internal/netdial"
)

// ReveniumMaxConnsPerHost caps the sockets one client opens to Revenium, so
// a slow Revenium queues requests instead of exhausting the process's file
// descriptors. Idle connections are kept up to the same number so a busy
// process reuses them instead of reconnecting. It also sizes core/metering's
// delivery workers and process-wide send slots, so changing it changes how
// many usage records the whole process sends at once.
const (
	ReveniumMaxConnsPerHost     = 32
	reveniumMaxIdleConns        = 100
	reveniumMaxIdleConnsPerHost = ReveniumMaxConnsPerHost
	reveniumIdleConnTimeout     = 90 * time.Second
)

// NewReveniumHTTPClient returns the HTTP client for calls to the Revenium API.
// It honours HTTP_PROXY, HTTPS_PROXY and NO_PROXY the same way the provider
// SDKs do, so a network that only reaches the internet through a proxy still
// delivers usage records, job outcomes and cost-control rules. Setting a
// DialContext turns off the automatic HTTP/2 upgrade, so it is forced back on
// to keep the connection sharing the transport had before the dial hook.
func NewReveniumHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			Proxy:               netdial.Proxy,
			DialContext:         netdial.DialContext,
			ForceAttemptHTTP2:   true,
			MaxIdleConns:        reveniumMaxIdleConns,
			MaxConnsPerHost:     ReveniumMaxConnsPerHost,
			MaxIdleConnsPerHost: reveniumMaxIdleConnsPerHost,
			IdleConnTimeout:     reveniumIdleConnTimeout,
			DisableCompression:  true,
		},
	}
}
