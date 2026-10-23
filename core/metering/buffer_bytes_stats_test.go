package metering

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuffer_StatsReportTheHeldBytesAndTheBudget(t *testing.T) {
	t.Setenv(core.EnvBufferMaxBytes, "1000000")
	revenium := newSwitchableRevenium(t)
	buf := NewMeteringBuffer(20000, time.Hour, 5*time.Second)
	defer buf.Stop()
	buf.path.monitor.onRecovery = nil
	buf.path.breaker = newTestBreaker()

	for i := 0; i < 20; i++ {
		buf.Push(capturedRecord(t, revenium.server.URL, "k"+strconv.Itoa(i)))
	}
	buf.flush()

	stats := buf.GetBufferStats()
	assert.Equal(t, int64(1000000), stats.MaxBytes)
	assertHeldBytesAccounted(t, buf, nil)

	revenium.down.Store(false)
	buf.flush()
	assert.Equal(t, int64(0), buf.GetBufferStats().HeldBytes)
}

func TestBuffer_DefaultsToTheDocumentedByteBudget(t *testing.T) {
	t.Setenv(core.EnvBufferMaxBytes, "")
	buf := NewMeteringBuffer(0, time.Hour, 5*time.Second)
	defer buf.Stop()

	assert.Equal(t, core.DefaultBufferMaxBytes, buf.GetBufferStats().MaxBytes)
}

func TestClient_BufferMaxBytesBoundsTheRecordsItHolds(t *testing.T) {
	t.Setenv(core.EnvBufferMaxBytes, "")
	client, err := NewMeteringClient(MeteringClientConfig{APIKey: "test-key", BaseURL: unreachableBaseURL(), BufferMaxBytes: 500000})
	require.NoError(t, err)
	defer func() { _ = client.Close() }()

	field := strings.Repeat("x", captureFieldSize)
	for i := 0; i < 10; i++ {
		holdUsageRecord(t, client, &MeteringPayload{
			TransactionID:  "tx-" + strconv.Itoa(i),
			SystemPrompt:   field,
			InputMessages:  field,
			OutputResponse: field,
		})
	}

	stats := client.GetBufferStats()
	assert.Equal(t, int64(500000), stats.MaxBytes)
	assert.LessOrEqual(t, stats.HeldBytes, int64(500000))
	assert.Equal(t, 3, stats.Size)
	assert.Equal(t, int64(7), stats.EventsEvicted)
}

func TestClientConfigFor_CarriesTheBufferByteLimitSetInCodeOverTheEnvironment(t *testing.T) {
	t.Setenv(core.EnvBufferMaxBytes, "2000000")
	rc := core.MergeReveniumConfig(&core.ReveniumConfig{APIKey: "hak_test_key", BaseURL: unreachableBaseURL(), BufferMaxBytes: 500000}, core.LoadReveniumConfig())

	client, err := NewMeteringClient(ClientConfigFor(rc))
	require.NoError(t, err)
	defer func() { _ = client.Close() }()

	assert.Equal(t, int64(500000), client.GetBufferStats().MaxBytes)
}

func TestClientConfigFor_TakesTheBufferByteLimitFromTheEnvironmentWhenCodeLeavesItUnset(t *testing.T) {
	t.Setenv(core.EnvBufferMaxBytes, "2000000")
	rc := core.MergeReveniumConfig(&core.ReveniumConfig{APIKey: "hak_test_key", BaseURL: unreachableBaseURL()}, core.LoadReveniumConfig())

	assert.Equal(t, int64(2000000), ClientConfigFor(rc).BufferMaxBytes)
}
