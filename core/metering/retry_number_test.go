package metering

import (
	"math"
	"strconv"
	"testing"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const retryNumberWarning = "dropping retryNumber"

func intAboveRetryLimit(t *testing.T) int {
	t.Helper()
	if strconv.IntSize < 64 {
		t.Skip("an int cannot exceed the 32-bit limit on this platform")
	}
	above := int64(math.MaxInt32) + 1
	return int(above)
}

func TestMeteringClient_OutOfRangeRetryNumberFromMetadataIsLeftOffAndTheRecordIsSent(t *testing.T) {
	shapes := map[string]func(t *testing.T) interface{}{
		"float above the 32-bit limit": func(*testing.T) interface{} { return float64(3e9) },
		"int above the 32-bit limit":   func(t *testing.T) interface{} { return intAboveRetryLimit(t) },
		"positive infinity":            func(*testing.T) interface{} { return math.Inf(1) },
		"negative infinity":            func(*testing.T) interface{} { return math.Inf(-1) },
		"NaN":                          func(*testing.T) interface{} { return math.NaN() },
		"negative int":                 func(*testing.T) interface{} { return -4 },
		"negative float":               func(*testing.T) interface{} { return float64(-4) },
		"float just above the limit":   func(*testing.T) interface{} { return float64(math.MaxInt32) + 1 },
	}
	for name, shape := range shapes {
		t.Run(name, func(t *testing.T) {
			clearRecordDefaults(t)
			logger := useRecordingLogger(t)

			record := recordSentBy(t, MeteringClientConfig{}, map[string]interface{}{retryNumberKey: shape(t), "environment": "staging"})

			require.NotNil(t, record, "the record still reaches Revenium")
			assert.NotContains(t, record, retryNumberKey)
			assert.Equal(t, "staging", record["environment"], "the other tags are kept")
			require.Len(t, warnLines(logger), 1)
			assert.Contains(t, warnLines(logger)[0], retryNumberWarning)
		})
	}
}

func TestMeteringClient_RetryNumbersInsideThe32BitRangeAreSentUnchanged(t *testing.T) {
	for _, value := range []interface{}{0, 7, math.MaxInt32, float64(math.MaxInt32), 2.9} {
		clearRecordDefaults(t)
		logger := useRecordingLogger(t)

		record := recordSentBy(t, MeteringClientConfig{}, map[string]interface{}{retryNumberKey: value})

		assert.Equal(t, math.Trunc(toFloat(value)), record[retryNumberKey], "%v", value)
		assert.Empty(t, warnLines(logger))
	}
}

func toFloat(value interface{}) float64 {
	if i, ok := value.(int); ok {
		return float64(i)
	}
	return value.(float64)
}

func TestSendSync_RetryNumberSetOnTheRecordOutsideTheRangeIsLeftOff(t *testing.T) {
	logger := useRecordingLogger(t)
	srv, requests := recordingServer(t)
	client := newTestClient(t, srv.URL)
	payload := NewPayload(OperationChat, "gpt-4", "OPENAI").Build()
	negative := -1
	payload.RetryNumber = &negative

	require.NoError(t, client.SendSync(payload))

	require.Len(t, requests(), 1)
	assert.NotContains(t, requests()[0].body, retryNumberKey)
	require.Len(t, warnLines(logger), 1)
	assert.Contains(t, warnLines(logger)[0], retryNumberWarning)
}

func TestMeteringClient_EnvironmentRetryNumberAboveTheLimitIsReportedOnceAndLeftOff(t *testing.T) {
	clearRecordDefaults(t)
	t.Setenv(core.EnvRetryNumber, "5000000000")
	logger := useRecordingLogger(t)
	srv, requests := recordingServer(t)
	client := newTestClient(t, srv.URL)

	for i := 0; i < 3; i++ {
		require.NoError(t, client.SendSync(NewPayload(OperationChat, "gpt-4", "OPENAI").Build()))
	}

	require.Len(t, requests(), 3)
	for _, request := range requests() {
		assert.NotContains(t, request.body, retryNumberKey)
	}
	require.Len(t, warnLines(logger), 1, "the default is checked once, not on every record")
	assert.Contains(t, warnLines(logger)[0], retryNumberWarning)
}
