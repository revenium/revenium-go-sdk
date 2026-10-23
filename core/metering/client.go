package metering

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/revenium/revenium-go-sdk/core/prompt"
	"github.com/revenium/revenium-go-sdk/core/resilience"
)

// sharedHTTPClient has no client-wide timeout on purpose: recordSender.post
// gives every request its own deadline, which is how the SDK's timeout is told
// apart from a caller's expired context. Send requests only through post.
var sharedHTTPClient = core.NewReveniumHTTPClient(0)

var afterSamplingRunningSends = func() {}

var (
	errNilUsageRecord = core.NewValidationError("usage record must not be nil", nil)
	errNilToolEvent   = core.NewValidationError("tool event payload must not be nil", nil)
)

const configuredBaseURLSetting = "the configured Revenium BaseURL"

type MeteringClientConfig struct {
	APIKey        string
	BaseURL       string
	BufferMaxSize int
	// BufferMaxBytes bounds the memory the held records and the records
	// waiting for their first send take together; zero means
	// REVENIUM_BUFFER_MAX_BYTES, or core.DefaultBufferMaxBytes when that is unset.
	BufferMaxBytes      int64
	BufferFlushInterval time.Duration
	// RequestTimeout bounds each request to Revenium; zero means
	// REVENIUM_API_TIMEOUT, or core.DefaultMeteringTimeout when that is unset.
	RequestTimeout time.Duration
	// OrganizationName and ProductName are the defaults for records whose
	// usage metadata names none; empty means the environment's, as
	// core.LoadReveniumConfig reads them.
	OrganizationName string
	ProductName      string
	// Debug turns on debug logging; false leaves the level another client
	// chose. REVENIUM_DEBUG reaches it through ClientConfigFor.
	Debug bool
}

// ClientConfigFor returns the client settings a provider module's merged
// Revenium config names: the account, the organization and product its
// records default to, and the memory its held records may take.
func ClientConfigFor(rc *core.ReveniumConfig) MeteringClientConfig {
	return MeteringClientConfig{
		APIKey:           rc.APIKey,
		BaseURL:          rc.BaseURL,
		OrganizationName: rc.OrgID,
		ProductName:      rc.ProductID,
		BufferMaxBytes:   rc.BufferMaxBytes,
		Debug:            rc.DebugRequested(),
	}
}

type MeteringClient struct {
	config         MeteringClientConfig
	defaults       map[string]interface{}
	retryConfig    *resilience.RetryConfig
	buffer         *MeteringBuffer
	path           *deliveryPath
	pool           *deliveryPool
	sends          activity
	overflowing    atomic.Bool
	releaseBreaker func()
}

func NewMeteringClient(cfg MeteringClientConfig) (*MeteringClient, error) {
	cfg.APIKey = strings.TrimSpace(cfg.APIKey)
	if cfg.APIKey == "" {
		return nil, core.NewConfigError("metering API key is required", nil)
	}
	if err := core.ValidateReveniumAPIKeyCharacters(cfg.APIKey); err != nil {
		return nil, err
	}
	baseURL, err := core.ResolveValidReveniumBaseURL(cfg.BaseURL, configuredBaseURLSetting)
	if err != nil {
		return nil, err
	}
	cfg.BaseURL = baseURL
	core.EnableDebugWhenRequested(cfg.Debug)
	if cfg.RequestTimeout <= 0 {
		cfg.RequestTimeout = core.MeteringTimeout()
	}
	breaker, releaseBreaker := resilience.AcquireCircuitBreaker(resilience.MeteringEndpoint, cfg.BaseURL, cfg.APIKey)
	return newMeteringClient(cfg, breaker, releaseBreaker), nil
}

func newMeteringClient(cfg MeteringClientConfig, breaker *resilience.CircuitBreaker, releaseBreaker func()) *MeteringClient {
	path := newDeliveryPath(breaker, senderWithTimeout(cfg.RequestTimeout))
	buffer := newMeteringBuffer(bufferLimits{records: cfg.BufferMaxSize, bytes: cfg.BufferMaxBytes}, cfg.BufferFlushInterval, max(defaultBufferFlushTimeout, cfg.RequestTimeout), path)
	path.monitor.onRecovery = buffer.requestReplay
	return &MeteringClient{
		config:         cfg,
		defaults:       recordDefaults(cfg),
		buffer:         buffer,
		path:           path,
		pool:           sharedDeliveryPool,
		releaseBreaker: releaseBreaker,
	}
}

// CircuitBreakerState reports whether records for this client's Revenium key
// are being sent (CLOSED) or held back while Revenium recovers (OPEN).
func (c *MeteringClient) CircuitBreakerState() resilience.State {
	return c.path.breaker.GetState()
}

func (c *MeteringClient) Send(payload *MeteringPayload) {
	if payload == nil {
		core.Error("failed to send metering data: %v", errNilUsageRecord)
		return
	}
	c.prepareRecord(payload)
	record, err := c.encodeUsageRecord(payload)
	if err != nil {
		core.Error("failed to send metering data: %v", err)
		return
	}
	c.enqueue(record, usageRecordKind)
}

func (c *MeteringClient) SendSync(payload *MeteringPayload) error {
	c.sends.begin()
	defer c.sends.end()
	if payload != nil {
		c.prepareRecord(payload)
	}
	return c.sendSync(payload)
}

func (c *MeteringClient) prepareRecord(payload *MeteringPayload) {
	fillUnsetFields(payload, c.defaults)
	payload.ErrorReason = truncateErrorReason(payload.ErrorReason)
	dropUnsendableRetryNumber(payload)
	payload.Subscriber = subscriberWithCredentialAlias(payload.Subscriber, payload.CredentialAlias)
}

func (c *MeteringClient) sendSync(payload *MeteringPayload) error {
	if payload == nil {
		return errNilUsageRecord
	}
	record, err := c.encodeUsageRecord(payload)
	if err != nil {
		return err
	}
	return c.holdIfRetryable(record, c.deliver(record, usageRecordKind))
}

func (c *MeteringClient) SendToolEvent(payload *ToolEventPayload) {
	if payload == nil {
		core.Error("failed to send tool event data: %v", errNilToolEvent)
		return
	}
	c.prepareToolEvent(payload)
	record, err := c.encodeToolEvent(payload)
	if err != nil {
		core.Error("failed to send tool event data: %v", err)
		return
	}
	c.enqueue(record, toolEventKind)
}

func (c *MeteringClient) SendToolEventSync(payload *ToolEventPayload) error {
	c.sends.begin()
	defer c.sends.end()
	if payload != nil {
		c.prepareToolEvent(payload)
	}
	return c.sendToolEventSync(payload)
}

func (c *MeteringClient) prepareToolEvent(payload *ToolEventPayload) {
	if jobID, ok := c.defaults["agenticJobId"].(string); ok && core.IsBlank(payload.AgenticJobID) {
		payload.AgenticJobID = jobID
	}
}

func (c *MeteringClient) sendToolEventSync(payload *ToolEventPayload) error {
	if payload == nil {
		return errNilToolEvent
	}
	record, err := c.encodeToolEvent(payload)
	if err != nil {
		return err
	}
	return c.holdIfRetryable(record, c.deliver(record, toolEventKind))
}

// enqueue hands an encoded record to the delivery pool. Its bytes count
// against the client's byte budget from here until it is delivered, dropped
// or held, so records waiting behind a slow Revenium are bounded as held
// ones are.
func (c *MeteringClient) enqueue(record BufferedEvent, kind recordKind) {
	reservation := c.buffer.reserveForSend(record.heldBytes())
	if reservation == nil {
		return
	}
	c.sends.begin()
	job := func() {
		defer c.sends.end()
		defer reservation.release()
		defer func() {
			if r := recover(); r != nil {
				core.Error("panic in metering send: %v", r)
			}
		}()
		c.reportQueuedSend(settleQueuedSend(reservation, record, c.deliver(record, kind)), kind)
	}
	if c.pool.trySubmit(job) {
		if c.pool.hasRoom() {
			c.overflowing.Store(false)
		}
		return
	}
	defer c.sends.end()
	if c.overflowing.CompareAndSwap(false, true) {
		core.Warn("[METERING] usage records are arriving faster than Revenium accepts them; holding new records in memory and resending them later")
	}
	reservation.hold(record)
}

// settleQueuedSend ends a queued record's reservation by its send's outcome:
// a failure worth retrying keeps the record for the next flush, anything else
// returns its bytes.
func settleQueuedSend(reservation *sendReservation, record BufferedEvent, err error) error {
	if isBufferable(err) {
		reservation.hold(record)
		return err
	}
	reservation.release()
	return err
}

func (c *MeteringClient) reportQueuedSend(err error, kind recordKind) {
	switch {
	case err == nil:
	case isBufferable(err):
		core.Debug("%s buffered for retry: %v", kind.label, err)
	default:
		core.Error("failed to send %s data: %v", kind.label, err)
	}
}

// deliver sends one encoded record with retries and reports the outcome to
// the delivery monitor.
func (c *MeteringClient) deliver(record BufferedEvent, kind recordKind) error {
	core.Debug("[METERING] Sending %s to %s: %s", kind.noun, redactURL(record.URL), string(record.Body))
	err := c.path.sendWithRetries(c.retryConfig, func() error {
		return c.path.sender.post(record.URL, record.Body, record.Headers, kind.label)
	})
	c.path.monitor.record(err)
	if err == nil {
		core.Debug("[METERING] %s sent successfully", kind.noun)
		kind.onDelivered(record.Body)
	}
	return err
}

func (c *MeteringClient) holdIfRetryable(record BufferedEvent, err error) error {
	if isBufferable(err) {
		c.buffer.Push(record)
	}
	return err
}

func printPayloadSummary(jsonData []byte) {
	format := prompt.ShouldPrintSummary()
	if format == "" {
		return
	}
	var m map[string]interface{}
	if err := json.Unmarshal(jsonData, &m); err != nil {
		return
	}
	prompt.PrintUsageSummary(m, format)
}

func (c *MeteringClient) Flush() {
	c.waitForSends(c.buffer.flushTimeout)
	c.buffer.drain(c.buffer.flushTimeout)
}

// Close gives records being sent, then held ones, a bounded time to reach
// Revenium, and returns an error counting any still undelivered, because they
// are lost when the process exits. Running sends are counted before held
// records, so a send that fails and holds its record while Close counts is
// never missed.
func (c *MeteringClient) Close() error {
	sent := c.waitForSends(c.buffer.flushTimeout)
	drained := c.buffer.stop()
	c.releaseBreakerWhenIdle(sent && drained)
	undelivered := c.undelivered()
	if undelivered == 0 {
		return nil
	}
	core.Warn("[METERING] %d usage records could not be delivered to Revenium before Close returned and are lost when the process exits", undelivered)
	return core.NewMeteringError(fmt.Sprintf("%d usage records were not delivered to Revenium", undelivered), nil)
}

func (c *MeteringClient) undelivered() int {
	running := c.sends.running()
	afterSamplingRunningSends()
	return running + c.buffer.Undelivered()
}

func (c *MeteringClient) releaseBreakerWhenIdle(idle bool) {
	if c.releaseBreaker == nil {
		return
	}
	if idle {
		c.releaseBreaker()
		return
	}
	sendsFinished := c.sends.finished()
	go func() {
		defer func() {
			if r := recover(); r != nil {
				core.Error("[METERING] panic while releasing the circuit breaker: %v", r)
			}
		}()
		<-sendsFinished
		c.buffer.waitUntilIdle()
		c.releaseBreaker()
	}()
}

func (c *MeteringClient) waitForSends(timeout time.Duration) bool {
	select {
	case <-c.sends.finished():
		return true
	case <-time.After(timeout):
		core.Warn("[METERING] %d usage records were still being sent after %s", c.sends.running(), timeout)
		return false
	}
}

func (c *MeteringClient) GetBufferStats() BufferStats {
	return c.buffer.GetBufferStats()
}

func (c *MeteringClient) buildHeadersMap(idempotencyKey string) map[string]string {
	h := map[string]string{
		"Content-Type": "application/json; charset=utf-8",
		"x-api-key":    c.config.APIKey,
		"User-Agent":   "revenium-go-sdk/1.0",
	}
	if idempotencyKey != "" {
		h["Idempotency-Key"] = idempotencyKey
	}
	return h
}

func (c *MeteringClient) encodeUsageRecord(payload *MeteringPayload) (BufferedEvent, error) {
	ensureSendableIdempotencyKey(&payload.IdempotencyKey)
	return c.encode(payload, MeteringEndpoint(c.config.BaseURL, OperationType(payload.OperationType)), payload.IdempotencyKey, "usage record")
}

func (c *MeteringClient) encodeToolEvent(payload *ToolEventPayload) (BufferedEvent, error) {
	ensureSendableIdempotencyKey(&payload.IdempotencyKey)
	return c.encode(payload, ToolEventEndpoint(c.config.BaseURL), payload.IdempotencyKey, "tool event")
}

func (c *MeteringClient) encode(payload interface{}, endpoint, idempotencyKey, what string) (BufferedEvent, error) {
	body, err := marshalRecord(payload)
	if err != nil {
		return BufferedEvent{}, core.NewValidationError(what+" could not be encoded as JSON and was dropped", err)
	}
	return BufferedEvent{
		URL:            endpoint,
		Headers:        c.buildHeadersMap(idempotencyKey),
		Body:           body,
		IdempotencyKey: idempotencyKey,
	}, nil
}

// marshalRecord encodes a record on the caller's goroutine, where Send needs
// its size for the byte budget before queueing it. A value the caller put in
// the record may have a MarshalJSON that panics; that panic becomes an error
// so it drops the record instead of failing the caller's request.
func marshalRecord(payload interface{}) (body []byte, err error) {
	defer func() {
		if r := recover(); r != nil {
			body, err = nil, fmt.Errorf("encoding panicked: %v", r)
		}
	}()
	return json.Marshal(payload)
}

func isBufferable(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, resilience.ErrCircuitOpen) {
		return true
	}
	return resilience.IsRetryable(resilience.ClassifyError(err))
}

// recordKind names a kind of record in requests and log lines, and says what
// to do once one is delivered.
type recordKind struct {
	label       string
	noun        string
	onDelivered func(body []byte)
}

var (
	usageRecordKind = recordKind{label: "metering", noun: "payload", onDelivered: printPayloadSummary}
	toolEventKind   = recordKind{label: "tool event", noun: "tool event", onDelivered: func([]byte) {}}
)
