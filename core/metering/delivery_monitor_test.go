package metering

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/revenium/revenium-go-sdk/core/resilience"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type recordingLogger struct {
	mu    sync.Mutex
	level core.LogLevel
	lines []string
}

func (l *recordingLogger) record(level core.LogLevel, message string, args ...interface{}) {
	if level < l.level {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lines = append(l.lines, level.String()+" "+fmt.Sprintf(message, args...))
}

func (l *recordingLogger) Debug(message string, args ...interface{}) {
	l.record(core.LogLevelDebug, message, args...)
}
func (l *recordingLogger) Info(message string, args ...interface{}) {
	l.record(core.LogLevelInfo, message, args...)
}
func (l *recordingLogger) Warn(message string, args ...interface{}) {
	l.record(core.LogLevelWarn, message, args...)
}
func (l *recordingLogger) Error(message string, args ...interface{}) {
	l.record(core.LogLevelError, message, args...)
}
func (l *recordingLogger) SetLevel(level core.LogLevel) { l.level = level }
func (l *recordingLogger) GetLevel() core.LogLevel      { return l.level }

func (l *recordingLogger) Lines() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.lines...)
}

func useRecordingLogger(t *testing.T) *recordingLogger {
	t.Helper()
	original := core.GetLogger()
	logger := &recordingLogger{level: core.LogLevelInfo}
	core.SetLogger(logger)
	t.Cleanup(func() { core.SetLogger(original) })
	return logger
}

func unreachableBaseURL() string {
	srv := httptest.NewServer(http.NotFoundHandler())
	srv.Close()
	return srv.URL
}

func TestSendSync_WarnsAtDefaultLevelWhenRecordsCannotBeDelivered(t *testing.T) {
	resilience.ResetCircuitBreakers()
	t.Cleanup(resilience.ResetCircuitBreakers)
	logger := useRecordingLogger(t)
	client := newTestClient(t, unreachableBaseURL())

	require.Error(t, client.SendSync(NewPayload(OperationChat, "gpt-4", "OPENAI").Build()))

	lines := recoveryLines(logger)
	require.Len(t, lines, 1)
	assert.Contains(t, lines[0], "WARN [METERING] usage records are not reaching Revenium")
}

func TestDeliveryMonitor_WarnsOncePerOutageAndReportsRecovery(t *testing.T) {
	logger := useRecordingLogger(t)
	networkErr := core.NewNetworkError("metering request failed", fmt.Errorf("dial tcp: no such host"))
	var monitor deliveryMonitor

	monitor.record(networkErr)
	monitor.record(networkErr)
	monitor.record(nil)
	monitor.record(nil)
	monitor.record(networkErr)

	lines := logger.Lines()
	require.Len(t, lines, 3)
	assert.Contains(t, lines[0], "WARN [METERING] usage records are not reaching Revenium")
	assert.Equal(t, "INFO [METERING] usage records are reaching Revenium again", lines[1])
	assert.Contains(t, lines[2], "WARN [METERING] usage records are not reaching Revenium")
}

func TestDeliveryMonitor_IgnoresRejectedRecords(t *testing.T) {
	logger := useRecordingLogger(t)
	var monitor deliveryMonitor

	monitor.record(core.NewValidationError("metering API returned 400", nil))

	assert.Empty(t, logger.Lines())
}

func failThenRecoverServer(t *testing.T) (*httptest.Server, *atomic.Bool) {
	t.Helper()
	var reachable atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !reachable.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	return srv, &reachable
}

func recoveryLines(logger *recordingLogger) []string {
	var lines []string
	for _, line := range logger.Lines() {
		if strings.Contains(line, "[METERING] usage records are") {
			lines = append(lines, line)
		}
	}
	return lines
}

func TestFlush_ReportsRecoveryWhenBufferedRecordsAreReplayed(t *testing.T) {
	resilience.ResetCircuitBreakers()
	t.Cleanup(resilience.ResetCircuitBreakers)
	logger := useRecordingLogger(t)
	srv, reachable := failThenRecoverServer(t)
	client := newTestClient(t, srv.URL)
	t.Cleanup(func() { _ = client.Close() })

	require.Error(t, client.SendSync(NewPayload(OperationChat, "gpt-4", "OPENAI").Build()))
	require.Equal(t, 1, client.GetBufferStats().Size)
	reachable.Store(true)
	client.Flush()

	assert.Equal(t, int64(1), client.GetBufferStats().EventsReplayed)
	lines := recoveryLines(logger)
	require.Len(t, lines, 2)
	assert.Contains(t, lines[0], "WARN [METERING] usage records are not reaching Revenium")
	assert.Equal(t, "INFO [METERING] usage records are reaching Revenium again", lines[1])
}

func TestReplayTimer_ReportsRecoveryWhenBufferedRecordsAreReplayed(t *testing.T) {
	resilience.ResetCircuitBreakers()
	t.Cleanup(resilience.ResetCircuitBreakers)
	logger := useRecordingLogger(t)
	srv, reachable := failThenRecoverServer(t)
	client, err := NewMeteringClient(MeteringClientConfig{APIKey: "test-key", BaseURL: srv.URL, BufferFlushInterval: 20 * time.Millisecond})
	require.NoError(t, err)
	client.retryConfig = testRetryConfig
	client.path.breaker = newTestBreaker()
	t.Cleanup(func() { _ = client.Close() })

	require.Error(t, client.SendSync(NewPayload(OperationChat, "gpt-4", "OPENAI").Build()))
	reachable.Store(true)

	require.Eventually(t, func() bool { return len(recoveryLines(logger)) == 2 }, 2*time.Second, 10*time.Millisecond)
	assert.Equal(t, "INFO [METERING] usage records are reaching Revenium again", recoveryLines(logger)[1])
}

func TestSendSync_OutageWarningNamesTheRealCauseNotTheBreaker(t *testing.T) {
	resilience.ResetCircuitBreakers()
	t.Cleanup(resilience.ResetCircuitBreakers)
	logger := useRecordingLogger(t)
	client, err := NewMeteringClient(MeteringClientConfig{APIKey: "test-key", BaseURL: unreachableBaseURL()})
	require.NoError(t, err)
	client.retryConfig = testRetryConfig
	t.Cleanup(func() { _ = client.Close() })

	require.Error(t, client.SendSync(NewPayload(OperationChat, "gpt-4", "OPENAI").Build()))
	require.ErrorIs(t, client.SendSync(NewPayload(OperationChat, "gpt-4", "OPENAI").Build()), resilience.ErrCircuitOpen)

	lines := recoveryLines(logger)
	require.Len(t, lines, 1)
	assert.Contains(t, lines[0], "connection refused")
}

func TestSendSync_OutageWarningHidesTheUserNameAndQueryOfTheReveniumAddress(t *testing.T) {
	resilience.ResetCircuitBreakers()
	t.Cleanup(resilience.ResetCircuitBreakers)
	logger := useRecordingLogger(t)
	address := strings.Replace(unreachableBaseURL(), "http://", "http://relayuser:relaypass@", 1) + "/v1?api_key=topsecretvalue"
	client := newTestClient(t, address)

	err := client.SendSync(NewPayload(OperationChat, "gpt-4", "OPENAI").Build())

	require.Error(t, err)
	lines := recoveryLines(logger)
	require.Len(t, lines, 1)
	assert.Contains(t, lines[0], "connection refused", "the cause is still named")
	for _, secret := range []string{"relayuser", "relaypass", "topsecretvalue"} {
		assert.NotContains(t, lines[0], secret)
		assert.NotContains(t, err.Error(), secret, "SendSync callers log this error too")
	}
}

func TestRedactURL_HidesUserInfoQueryAndFragmentButKeepsTheEndpoint(t *testing.T) {
	cases := map[string]string{
		"http://user:pass@relay.example:8080/v1/meter/v2/ai/completions": "http://REDACTED@relay.example:8080/v1/meter/v2/ai/completions",
		"https://relay.example/v1?api_key=secret&team=t1":                "https://relay.example/v1?REDACTED",
		"https://relay.example/v1?bare-token-value":                      "https://relay.example/v1?REDACTED",
		"https://onlyuser@relay.example/v1#frag-secret":                  "https://REDACTED@relay.example/v1",
		"https://api.revenium.ai/meter/v2/ai/completions":                "https://api.revenium.ai/meter/v2/ai/completions",
		"http://[::1%25zone/broken":                                      "REDACTED",
	}
	for raw, want := range cases {
		assert.Equal(t, want, redactURL(raw), raw)
	}
}

func TestSendSync_DebugLinesHideTheUserNameAndQueryOfTheReveniumAddress(t *testing.T) {
	srv, _ := recordingServer(t)
	logger := useRecordingLogger(t)
	logger.SetLevel(core.LogLevelDebug)
	address := strings.Replace(srv.URL, "http://", "http://relayuser:relaypass@", 1) + "/v1?api_key=topsecretvalue"
	client := newTestClient(t, address)
	t.Cleanup(func() { _ = client.Close() })

	require.NoError(t, client.SendSync(NewPayload(OperationChat, "gpt-4", "OPENAI").Build()))

	lines := logger.Lines()
	require.NotZero(t, countLinesContaining(logger, "Sending payload to"), "the debug line naming the address is logged")
	for _, line := range lines {
		for _, secret := range []string{"relayuser", "relaypass", "topsecretvalue"} {
			assert.NotContains(t, line, secret)
		}
	}
}
