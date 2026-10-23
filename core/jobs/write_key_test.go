package jobs

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewJobClient_RefusesAMeteringKeyAndNamesTheWriteKey(t *testing.T) {
	client, err := NewJobClient(JobClientConfig{APIKey: "rev_mk_abc123", TeamID: "team-1"})

	assert.Nil(t, client)
	require.Error(t, err)
	assert.True(t, core.IsConfigError(err))
	assert.Contains(t, err.Error(), "write key (rev_sk_)")
	assert.Contains(t, err.Error(), "REVENIUM_WRITE_API_KEY")
}

func TestNewJobClient_MissingKeyAsksForAWriteKey(t *testing.T) {
	_, err := NewJobClient(JobClientConfig{TeamID: "team-1"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "write API key (rev_sk_)")
	assert.NotContains(t, err.Error(), "metering API key")
}

func TestNewJobClient_RejectsAKeyThatCannotTravelInAHeader(t *testing.T) {
	client, err := NewJobClient(JobClientConfig{APIKey: "rev_sk_abc\n123", TeamID: "team-1"})

	assert.Nil(t, client)
	require.Error(t, err)
	assert.True(t, core.IsConfigError(err))
	assert.Contains(t, err.Error(), "JobClientConfig.APIKey")
	assert.NotContains(t, err.Error(), "abc", "the key is never echoed")
}

func TestNewJobClient_RejectsAKeyThatIsNotAReveniumKey(t *testing.T) {
	client, err := NewJobClient(JobClientConfig{APIKey: "sk-not-a-revenium-key", TeamID: "team-1"})

	assert.Nil(t, client)
	require.Error(t, err)
	assert.True(t, core.IsConfigError(err))
	assert.Contains(t, err.Error(), "rev_sk_")
	assert.NotContains(t, err.Error(), "sk-not-a-revenium-key", "the key is never echoed")
}

func TestNewJobClient_AcceptsAWriteKey(t *testing.T) {
	client, err := NewJobClient(JobClientConfig{APIKey: "rev_sk_abc123", TeamID: "team-1"})

	require.NoError(t, err)
	t.Cleanup(client.Close)
}

func TestJobCall_ScopeRefusalIsAnAuthorizationErrorThatNamesTheWriteKey(t *testing.T) {
	cases := []struct {
		name   string
		status int
	}{
		{"metering key refused", http.StatusForbidden},
		{"unknown key", http.StatusUnauthorized},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var hits atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				hits.Add(1)
				w.Header().Set("Retry-After", "2")
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(`{"status":403,"error":"Forbidden","message":"Insufficient API key scope"}`))
			}))
			t.Cleanup(srv.Close)
			client := newTestClient(t, srv.URL)
			t.Cleanup(client.Close)

			_, err := client.ReportJobOutcome("job-1", &JobOutcome{ExecutionStatus: ExecutionStatusSuccess})

			require.Error(t, err)
			assert.True(t, core.IsAuthError(err), "got %v", err)
			assert.False(t, core.IsValidationError(err))
			var revErr *core.ReveniumError
			require.True(t, errors.As(err, &revErr))
			assert.Equal(t, tc.status, revErr.StatusCode)
			assert.Equal(t, 2*time.Second, revErr.RetryAfter.Round(time.Second), "the shared mapper reads Retry-After")
			assert.Contains(t, err.Error(), fmt.Sprintf("job API returned %d", tc.status))
			assert.Contains(t, err.Error(), "write key (rev_sk_)")
			assert.Contains(t, err.Error(), "Insufficient API key scope")
			assert.Equal(t, int32(1), hits.Load(), "a refused key is not retried")
		})
	}
}
