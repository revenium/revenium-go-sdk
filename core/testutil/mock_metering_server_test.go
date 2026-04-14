package testutil

import (
	"bytes"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func post(t *testing.T, url, body string) *http.Response {
	t.Helper()
	req, err := http.NewRequest("POST", url, bytes.NewBufferString(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	return resp
}

// postNoFail is goroutine-safe: it never calls t.FailNow() so it can be invoked
// from non-test goroutines (e.g., httptest handlers, background producers).
func postNoFail(t *testing.T, url, body string) {
	t.Helper()
	req, err := http.NewRequest("POST", url, bytes.NewBufferString(body))
	if err != nil {
		t.Errorf("postNoFail new request: %v", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Errorf("postNoFail do: %v", err)
		return
	}
	resp.Body.Close()
}

func TestMockMeteringServer_CapturesJSONPayload(t *testing.T) {
	m := NewMockMeteringServer()
	defer m.Close()

	resp := post(t, m.URL()+"/meter/v2/ai/completions", `{"operationType":"CHAT","model":"gpt-4"}`)
	defer resp.Body.Close()
	assert.Equal(t, 200, resp.StatusCode)

	payloads := m.GetPayloads()
	require.Len(t, payloads, 1)
	assert.Equal(t, "CHAT", payloads[0]["operationType"])
	assert.Equal(t, "gpt-4", payloads[0]["model"])

	requests := m.GetRequests()
	require.Len(t, requests, 1)
	assert.Equal(t, "/meter/v2/ai/completions", requests[0].Path)
	assert.Equal(t, "POST", requests[0].Method)
}

func TestMockMeteringServer_Reset(t *testing.T) {
	m := NewMockMeteringServer()
	defer m.Close()

	post(t, m.URL(), `{"x":1}`).Body.Close()
	require.Len(t, m.GetPayloads(), 1)

	m.Reset()
	assert.Len(t, m.GetPayloads(), 0)
	assert.Len(t, m.GetRequests(), 0)
}

func TestMockMeteringServer_SetFailure(t *testing.T) {
	m := NewMockMeteringServer()
	defer m.Close()

	m.SetFailure(500, `{"error":"boom"}`)
	resp := post(t, m.URL(), `{"a":1}`)
	defer resp.Body.Close()
	assert.Equal(t, 500, resp.StatusCode)

	require.Len(t, m.GetPayloads(), 1, "payload still captured before failure response")

	m.SetFailure(0, "")
	resp2 := post(t, m.URL(), `{"a":2}`)
	defer resp2.Body.Close()
	assert.Equal(t, 200, resp2.StatusCode)
}

func TestMockMeteringServer_SetLatency(t *testing.T) {
	m := NewMockMeteringServer()
	defer m.Close()

	m.SetLatency(50 * time.Millisecond)
	start := time.Now()
	resp := post(t, m.URL(), `{}`)
	resp.Body.Close()
	assert.GreaterOrEqual(t, time.Since(start), 50*time.Millisecond)
}

func TestMockMeteringServer_WaitForPayloads(t *testing.T) {
	m := NewMockMeteringServer()
	defer m.Close()

	go func() {
		time.Sleep(20 * time.Millisecond)
		postNoFail(t, m.URL(), `{"k":1}`)
		postNoFail(t, m.URL(), `{"k":2}`)
	}()

	assert.True(t, m.WaitForPayloads(2, 500*time.Millisecond))

	empty := NewMockMeteringServer()
	defer empty.Close()
	assert.False(t, empty.WaitForPayloads(1, 50*time.Millisecond))
}
