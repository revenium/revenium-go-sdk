package metering

import (
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf16"

	"github.com/revenium/revenium-go-sdk/core/resilience"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type recordedRequest struct {
	idempotencyKey string
	body           map[string]interface{}
}

func recordingServer(t *testing.T) (*httptest.Server, func() []recordedRequest) {
	t.Helper()
	var mu sync.Mutex
	var requests []recordedRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		mu.Lock()
		requests = append(requests, recordedRequest{idempotencyKey: r.Header.Get("Idempotency-Key"), body: body})
		mu.Unlock()
		w.WriteHeader(http.StatusCreated)
	}))
	t.Cleanup(srv.Close)
	return srv, func() []recordedRequest {
		mu.Lock()
		defer mu.Unlock()
		return append([]recordedRequest(nil), requests...)
	}
}

func warnLines(logger *recordingLogger) []string {
	var lines []string
	for _, line := range logger.Lines() {
		if strings.HasPrefix(line, "WARN ") || strings.HasPrefix(line, "ERROR ") {
			lines = append(lines, line)
		}
	}
	return lines
}

func TestApplyMetadata_ShortensTicketIDToPlatformLimit(t *testing.T) {
	logger := useRecordingLogger(t)
	p := NewPayload(OperationChat, "gpt-4", "OPENAI").Build()

	ApplyMetadata(p, map[string]interface{}{"ticketId": strings.Repeat("T", 300)})

	assert.Equal(t, strings.Repeat("T", 256), p.TicketID)
	require.Len(t, warnLines(logger), 1)
	assert.Contains(t, warnLines(logger)[0], "ticketId")
}

func TestApplyMetadata_ShortensTicketIDByPlatformCharacterCount(t *testing.T) {
	p := NewPayload(OperationChat, "gpt-4", "OPENAI").Build()

	ApplyMetadata(p, map[string]interface{}{"ticketId": strings.Repeat("\U0001F600", 200)})

	assert.Len(t, utf16.Encode([]rune(p.TicketID)), 256)
}

func TestApplyMetadata_KeepsTicketIDAtLimit(t *testing.T) {
	logger := useRecordingLogger(t)
	p := NewPayload(OperationChat, "gpt-4", "OPENAI").Build()
	ticket := strings.Repeat("é", 256)

	ApplyMetadata(p, map[string]interface{}{"ticketId": ticket})

	assert.Equal(t, ticket, p.TicketID)
	assert.Empty(t, warnLines(logger))
}

func TestApplyMetadata_DropsNonFiniteNumbersWithWarningAndKeepsTheRest(t *testing.T) {
	values := map[string]interface{}{
		"float NaN":       math.NaN(),
		"float +Inf":      math.Inf(1),
		"float32 -Inf":    float32(math.Inf(-1)),
		"string NaN":      "NaN",
		"string Inf":      "Inf",
		"string Infinity": "-Infinity",
	}
	for name, value := range values {
		t.Run(name, func(t *testing.T) {
			logger := useRecordingLogger(t)
			p := NewPayload(OperationChat, "gpt-4", "OPENAI").Build()

			ApplyMetadata(p, map[string]interface{}{
				"responseQualityScore": value,
				"temperature":          0.4,
				"traceId":              "trace-1",
			})

			assert.Nil(t, p.ResponseQualityScore)
			require.NotNil(t, p.Temperature)
			assert.Equal(t, 0.4, *p.Temperature)
			assert.Equal(t, "trace-1", p.TraceID)
			_, err := json.Marshal(p)
			require.NoError(t, err)
			require.Len(t, warnLines(logger), 1)
			assert.Contains(t, warnLines(logger)[0], "responseQualityScore")
		})
	}
}

func TestApplyMetadata_IgnoresUnsendableIdempotencyKeyAndKeepsGeneratedOne(t *testing.T) {
	keys := []string{"order-42\n", "order\r\nX-Injected: 1", strings.Repeat("k", 256), "commande-é", " padded ", "order 42"}
	for _, key := range keys {
		logger := useRecordingLogger(t)
		p := NewPayload(OperationChat, "gpt-4", "OPENAI").Build()
		generated := p.IdempotencyKey

		ApplyMetadata(p, map[string]interface{}{"idempotencyKey": key})

		assert.Equal(t, generated, p.IdempotencyKey, "%q", key)
		assert.Len(t, warnLines(logger), 1, "%q", key)
	}
}

func TestApplyMetadata_AcceptsPrintableASCIIIdempotencyKey(t *testing.T) {
	p := NewPayload(OperationChat, "gpt-4", "OPENAI").Build()
	key := "order-42:retry#1/" + strings.Repeat("x", 237)

	ApplyMetadata(p, map[string]interface{}{"idempotencyKey": key})

	assert.Equal(t, key, p.IdempotencyKey)
}

func TestSendSync_ReplacesUnsendableIdempotencyKeyAndDeliversTheRecord(t *testing.T) {
	resilience.ResetCircuitBreakers()
	t.Cleanup(resilience.ResetCircuitBreakers)
	logger := useRecordingLogger(t)
	srv, requests := recordingServer(t)
	client := newTestClient(t, srv.URL)

	err := client.SendSync(NewPayload(OperationChat, "gpt-4", "OPENAI").WithIdempotencyKey("order-42\n").Build())

	require.NoError(t, err)
	require.Len(t, requests(), 1)
	assert.NotEmpty(t, requests()[0].idempotencyKey)
	assert.NotContains(t, requests()[0].idempotencyKey, "order-42")
	require.Len(t, warnLines(logger), 1)
	assert.Contains(t, warnLines(logger)[0], "idempotency key")
}

func TestSendToolEventSync_ReplacesUnsendableIdempotencyKeyAndDeliversTheEvent(t *testing.T) {
	resilience.ResetCircuitBreakers()
	t.Cleanup(resilience.ResetCircuitBreakers)
	srv, requests := recordingServer(t)
	client := newTestClient(t, srv.URL)

	err := client.SendToolEventSync(NewToolEvent("web_scraper").WithIdempotencyKey("tool\n").Build())

	require.NoError(t, err)
	require.Len(t, requests(), 1)
	assert.NotEmpty(t, requests()[0].idempotencyKey)
}

func TestSendSync_RecordThatCannotBeEncodedIsDroppedLoudlyWithoutTrippingTheBreaker(t *testing.T) {
	resilience.ResetCircuitBreakers()
	t.Cleanup(resilience.ResetCircuitBreakers)
	logger := useRecordingLogger(t)
	srv, requests := recordingServer(t)
	client := newTestClient(t, srv.URL)

	for i := 0; i < 6; i++ {
		p := NewPayload(OperationChat, "gpt-4", "OPENAI").Build()
		p.Subscriber = map[string]interface{}{"score": math.NaN()}
		client.Send(p)
	}
	client.Flush()

	assert.Empty(t, requests())
	assert.Equal(t, 0, client.GetBufferStats().Size)
	assert.Equal(t, resilience.StateClosed, client.CircuitBreakerState())
	errors := warnLines(logger)
	require.Len(t, errors, 6)
	assert.Contains(t, errors[0], "could not be encoded")

	require.NoError(t, client.SendSync(NewPayload(OperationChat, "gpt-4", "OPENAI").Build()))
	assert.Len(t, requests(), 1)
}

func TestFlush_UnsendableRecordDoesNotHoldBackTheRecordsBehindIt(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusCreated)
	}))
	t.Cleanup(srv.Close)
	logger := useRecordingLogger(t)
	buf := newTestBuffer(100)
	t.Cleanup(buf.Stop)
	buf.Push(BufferedEvent{URL: srv.URL, Headers: map[string]string{"Idempotency-Key": "order-42\n"}, Body: []byte(`{}`)})
	for i := 0; i < 10; i++ {
		buf.Push(BufferedEvent{URL: srv.URL, Headers: map[string]string{"Idempotency-Key": GenerateTransactionID()}, Body: []byte(`{}`)})
	}

	buf.flush()

	assert.Equal(t, int32(10), hits.Load())
	assert.Equal(t, 0, buf.GetBufferStats().Size)
	require.Len(t, warnLines(logger), 1)
	assert.Contains(t, warnLines(logger)[0], "cannot be delivered")
}

func TestBuffer_ExpiredRecordsAreReportedAtWarn(t *testing.T) {
	logger := useRecordingLogger(t)
	buf := newTestBuffer(10)
	buf.mu.Lock()
	buf.events = append(buf.events, BufferedEvent{URL: "http://localhost/x", CreatedAt: time.Now().Add(-25 * time.Hour)})
	buf.evictExpired()
	buf.mu.Unlock()

	require.Len(t, warnLines(logger), 1)
	assert.Contains(t, warnLines(logger)[0], "dropped 1 usage records")
}

func TestHeldRecord_AnUnsendableIdempotencyKeyIsReplacedBeforeTheRecordIsHeld(t *testing.T) {
	srv, requests := recordingServer(t)
	client := newTestClient(t, srv.URL)
	t.Cleanup(func() { _ = client.Close() })
	payload := NewPayload(OperationChat, "gpt-4", "OPENAI").WithIdempotencyKey("order-42\n").Build()

	holdUsageRecord(t, client, payload)
	client.Flush()

	require.Len(t, requests(), 1, "the held record is resent instead of being dropped for its key")
	assert.NotContains(t, requests()[0].idempotencyKey, "order-42")
}

func TestSendSync_KeyWithAnInnerSpaceIsReplacedBeforeReveniumRejectsIt(t *testing.T) {
	srv, requests := recordingServer(t)
	client := newTestClient(t, srv.URL)

	require.NoError(t, client.SendSync(NewPayload(OperationChat, "gpt-4", "OPENAI").WithIdempotencyKey("user 42 / req 7").Build()))

	require.Len(t, requests(), 1)
	assert.NotContains(t, requests()[0].idempotencyKey, " ")
	assert.NotEmpty(t, requests()[0].idempotencyKey)
}

func TestSendSync_RecordBuiltWithoutAnIdempotencyKeyGetsOne(t *testing.T) {
	srv, requests := recordingServer(t)
	client := newTestClient(t, srv.URL)
	payload := &MeteringPayload{OperationType: string(OperationChat), Model: "gpt-4", Provider: "OPENAI", TransactionID: GenerateTransactionID()}

	require.NoError(t, client.SendSync(payload))

	require.Len(t, requests(), 1)
	assert.NotEmpty(t, requests()[0].idempotencyKey, "a resend after a timeout can then be recognised as the same record")
	assert.Equal(t, payload.IdempotencyKey, requests()[0].idempotencyKey)
}

func TestApplyToolEventMetadata_UsesTheSameGuardsAsUsageRecords(t *testing.T) {
	payload := NewToolEvent("web_scraper").Build()
	generated := payload.IdempotencyKey

	ApplyToolEventMetadata(payload, map[string]interface{}{"idempotencyKey": "order 42", "agenticJobId": "types"})

	assert.Equal(t, generated, payload.IdempotencyKey)
	assert.Empty(t, payload.AgenticJobID)
}

func TestSend_RecordThatCannotBeEncodedIsLoggedAsAnErrorOnTheOverflowPathToo(t *testing.T) {
	logger := useRecordingLogger(t)
	srv, _ := recordingServer(t)
	client := newTestClient(t, srv.URL)
	client.pool = newDeliveryPool(0, 0)
	p := NewPayload(OperationChat, "gpt-4", "OPENAI").Build()
	p.Subscriber = map[string]interface{}{"score": math.NaN()}

	client.Send(p)

	var errorsLogged int
	for _, line := range logger.Lines() {
		if strings.HasPrefix(line, "ERROR ") && strings.Contains(line, "could not be encoded") {
			errorsLogged++
		}
	}
	assert.Equal(t, 1, errorsLogged)
}
