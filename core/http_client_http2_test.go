package core

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestNewReveniumHTTPClient_KeepsHTTP2WithTheDialHook(t *testing.T) {
	transport, ok := NewReveniumHTTPClient(time.Second).Transport.(*http.Transport)
	require.True(t, ok)
	require.NotNil(t, transport.DialContext)
	require.True(t, transport.ForceAttemptHTTP2, "a custom DialContext turns off the automatic HTTP/2 upgrade unless it is forced")
}
