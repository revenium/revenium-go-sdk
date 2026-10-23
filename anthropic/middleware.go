package anthropic

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/revenium/revenium-go-sdk/core/enforcement"
	"github.com/revenium/revenium-go-sdk/core/metering"
	"github.com/revenium/revenium-go-sdk/core/prompt"
	"github.com/revenium/revenium-go-sdk/core/resilience"
)

type ReveniumAnthropic struct {
	client   anthropic.Client
	config   *Config
	provider Provider
	mu       sync.RWMutex
	metering *metering.MeteringClient
	enforcer *enforcement.Engine
}

var sharedClient = core.NewSharedClient("NewReveniumAnthropic", func(c *ReveniumAnthropic) *core.ReveniumConfig { return c.config.Revenium })

type AnthropicStatus struct {
	Initialized         bool
	HasConfig           bool
	Provider            string
	CircuitBreakerState string
}

// Initialize sets up the shared client for one Revenium account. A repeat
// call, including one made while the first setup is still running, returns
// nil when it names the same Revenium key, base URL and team, or none of
// them, and a config error otherwise; use NewReveniumAnthropic for several
// accounts in one process.
func Initialize(opts ...Option) error {
	cfg := &Config{}
	for _, opt := range opts {
		opt(cfg)
	}
	return sharedClient.Initialize(core.NamedAccount(cfg.Revenium), func() (*ReveniumAnthropic, error) {
		return buildSharedClient(cfg)
	})
}

func buildSharedClient(cfg *Config) (*ReveniumAnthropic, error) {
	core.InitializeLogger()
	core.Info("Initializing Revenium middleware...")

	if err := cfg.loadFromEnv(); err != nil {
		core.Warn("Failed to load configuration from environment: %v", err)
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	clientOpts := []option.RequestOption{}
	if cfg.AnthropicAPIKey != "" {
		clientOpts = append(clientOpts, option.WithAPIKey(cfg.AnthropicAPIKey))
	}

	anthropicClient := anthropic.NewClient(clientOpts...)
	provider := DetectProvider(cfg)

	mc, err := metering.NewMeteringClient(metering.ClientConfigFor(cfg.Revenium))
	if err != nil {
		return nil, err
	}

	candidate := &ReveniumAnthropic{
		client:   anthropicClient,
		config:   cfg,
		provider: provider,
		metering: mc,
		enforcer: enforcement.StartFor(cfg.Revenium),
	}

	core.Info("Revenium middleware initialized successfully")
	return candidate, nil
}

func IsInitialized() bool {
	return sharedClient.IsInitialized()
}

func GetClient() (*ReveniumAnthropic, error) {
	return sharedClient.Get()
}

func NewReveniumAnthropic(cfg *Config) (*ReveniumAnthropic, error) {
	if cfg == nil {
		return nil, core.NewConfigError("config cannot be nil", nil)
	}
	core.TrimReveniumConfig(cfg.Revenium)

	if cfg.Revenium == nil || cfg.Revenium.APIKey == "" {
		return nil, core.NewConfigError("REVENIUM_METERING_API_KEY is required", nil)
	}

	clientOpts := []option.RequestOption{}
	if cfg.AnthropicAPIKey != "" {
		clientOpts = append(clientOpts, option.WithAPIKey(cfg.AnthropicAPIKey))
	}

	anthropicClient := anthropic.NewClient(clientOpts...)
	provider := DetectProvider(cfg)

	mc, err := metering.NewMeteringClient(metering.ClientConfigFor(cfg.Revenium))
	if err != nil {
		return nil, err
	}

	return &ReveniumAnthropic{
		client:   anthropicClient,
		config:   cfg,
		provider: provider,
		metering: mc,
		enforcer: enforcement.StartFor(cfg.Revenium),
	}, nil
}

func (r *ReveniumAnthropic) GetConfig() *Config {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.config
}

func (r *ReveniumAnthropic) GetProvider() Provider {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.provider
}

// GetAnthropicClient returns the provider's own client. Calls made through it
// are neither recorded nor checked against cost controls.
func (r *ReveniumAnthropic) GetAnthropicClient() anthropic.Client {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.client
}

func (r *ReveniumAnthropic) GetStatus() AnthropicStatus {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return AnthropicStatus{
		Initialized:         IsInitialized(),
		HasConfig:           r.config != nil,
		Provider:            string(r.provider),
		CircuitBreakerState: r.metering.CircuitBreakerState().String(),
	}
}

func (r *ReveniumAnthropic) Messages() *MessagesInterface {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return &MessagesInterface{
		client:   r.client,
		config:   r.config,
		provider: r.provider,
		parent:   r,
	}
}

func (r *ReveniumAnthropic) Flush() {
	r.metering.Flush()
}

func (r *ReveniumAnthropic) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.enforcer.Release()
	return r.metering.Close()
}

type MessagesInterface struct {
	client   anthropic.Client
	config   *Config
	provider Provider
	parent   *ReveniumAnthropic
}

func (m *MessagesInterface) CreateMessage(ctx context.Context, params anthropic.MessageNewParams) (*anthropic.Message, error) {
	metadata := core.GetUsageMetadata(ctx)

	switch m.provider {
	case ProviderAnthropic:
		return m.createMessageAnthropic(ctx, params, metadata)
	case ProviderBedrock:
		return m.createMessageBedrock(ctx, params, metadata)
	default:
		return nil, core.NewProviderError("unknown provider: %v", fmt.Errorf("provider: %v", m.provider))
	}
}

func (m *MessagesInterface) CreateMessageStream(ctx context.Context, params anthropic.MessageNewParams) (interface{}, error) {
	metadata := core.GetUsageMetadata(ctx)

	switch m.provider {
	case ProviderAnthropic:
		return m.createMessageStreamAnthropic(ctx, params, metadata)
	case ProviderBedrock:
		return m.createMessageStreamBedrock(ctx, params, metadata)
	default:
		return nil, core.NewProviderError("unknown provider: %v", fmt.Errorf("provider: %v", m.provider))
	}
}

func (m *MessagesInterface) createMessageAnthropic(ctx context.Context, params anthropic.MessageNewParams, metadata map[string]interface{}) (*anthropic.Message, error) {
	startTime := time.Now()

	originalModel := string(params.Model)
	convertedModel, err := ConvertBedrockARNToAnthropicModel(originalModel)
	if err != nil {
		return nil, fmt.Errorf("failed to convert Bedrock model to Anthropic format: %w", err)
	}
	if convertedModel != originalModel {
		core.Info("Converted Bedrock model '%s' to Anthropic model '%s'", originalModel, convertedModel)
		params.Model = anthropic.Model(convertedModel)
	}

	if err := m.parent.enforcer.Check(enforcement.NewEvalContext(ctx, string(params.Model), NormalizeProviderName("Anthropic")).ServedModelMayBeDated()); err != nil {
		core.Debug("createMessageAnthropic blocked by enforcement: %v", err)
		return nil, err
	}

	hasVision := DetectVisionContent(params.Messages)

	resp, err := m.client.Messages.New(ctx, params)
	if err != nil {
		sendFailedCall(m.parent.metering, string(params.Model), "Anthropic", startTime, hasVision, metadata, err)
		return nil, err
	}

	turn := messageTurnUsage(resp)
	payload := buildTurnPayload(turn, turnTiming{start: startTime, duration: time.Since(startTime)}, "Anthropic", hasVision, metadata)
	metering.ApplyChatCapture(payload, metadata, func() metering.ChatCapture { return messageCapture(params, messageReply(resp.Content)) })
	sendTurnRecords(m.parent.metering, payload, turn.passes, "")

	return resp, nil
}

func (m *MessagesInterface) createMessageBedrock(ctx context.Context, params anthropic.MessageNewParams, metadata map[string]interface{}) (*anthropic.Message, error) {
	startTime := time.Now()
	recordedModel := m.recordedBedrockModelFor(params)

	if err := m.parent.enforcer.Check(enforcement.NewEvalContext(ctx, recordedModel, NormalizeProviderName("AWS"))); err != nil {
		core.Debug("createMessageBedrock blocked by enforcement: %v", err)
		return nil, err
	}

	bedrockAdapter, err := NewBedrockAdapter(m.config)
	if err != nil {
		return m.bedrockFailed(ctx, params, metadata, nil, err)
	}

	hasVision := DetectVisionContent(params.Messages)

	var resp *anthropic.Message

	err = resilience.WithRetry(ctx, func() error {
		var bedrockErr error
		if m.config.BedrockUseConverse {
			resp, bedrockErr = bedrockAdapter.CreateMessageConverse(ctx, params)
		} else {
			resp, bedrockErr = bedrockAdapter.CreateMessage(ctx, params)
		}
		return bedrockErr
	}, nil)

	if err != nil {
		return m.bedrockFailed(ctx, params, metadata, &bedrockCall{model: recordedModel, start: startTime, hasVision: hasVision}, err)
	}

	turn := messageTurnUsage(resp)
	turn.model = recordedModel
	payload := buildTurnPayload(turn, turnTiming{start: startTime, duration: time.Since(startTime)}, "AWS", hasVision, metadata)
	metering.ApplyChatCapture(payload, metadata, func() metering.ChatCapture { return messageCapture(params, messageReply(resp.Content)) })
	sendTurnRecords(m.parent.metering, payload, turn.passes, "")

	return resp, nil
}

func (m *MessagesInterface) createMessageStreamAnthropic(ctx context.Context, params anthropic.MessageNewParams, metadata map[string]interface{}) (interface{}, error) {
	originalModel := string(params.Model)
	convertedModel, err := ConvertBedrockARNToAnthropicModel(originalModel)
	if err != nil {
		return nil, fmt.Errorf("failed to convert Bedrock model to Anthropic format: %w", err)
	}
	if convertedModel != originalModel {
		core.Info("Converted Bedrock model '%s' to Anthropic model '%s' for streaming", originalModel, convertedModel)
		params.Model = anthropic.Model(convertedModel)
	}

	if err := m.parent.enforcer.Check(enforcement.NewEvalContext(ctx, string(params.Model), NormalizeProviderName("Anthropic")).ServedModelMayBeDated()); err != nil {
		core.Debug("createMessageStreamAnthropic blocked by enforcement: %v", err)
		return nil, err
	}

	hasVision := DetectVisionContent(params.Messages)
	startTime := time.Now()
	stream := m.client.Messages.NewStreaming(ctx, params)

	streamMetadata := buildStreamMetadata(metadata, string(params.Model))
	wrapper := newStreamingWrapper(stream, m.config, streamMetadata, m, string(params.Model), "Anthropic", hasVision, startTime)
	wrapper.captureRequest(params)

	inputTokens := estimateInputTokens(params)
	wrapper.SetInputTokens(inputTokens)

	return wrapper, nil
}

func (m *MessagesInterface) createMessageStreamBedrock(ctx context.Context, params anthropic.MessageNewParams, metadata map[string]interface{}) (interface{}, error) {
	recordedModel := m.recordedBedrockModelFor(params)
	if err := m.parent.enforcer.Check(enforcement.NewEvalContext(ctx, recordedModel, NormalizeProviderName("AWS"))); err != nil {
		core.Debug("createMessageStreamBedrock blocked by enforcement: %v", err)
		return nil, err
	}

	bedrockAdapter, err := NewBedrockAdapter(m.config)
	if err != nil {
		return m.bedrockStreamFailed(ctx, params, metadata, nil, err)
	}

	hasVision := DetectVisionContent(params.Messages)
	startTime := time.Now()

	var stream interface{}

	err = resilience.WithRetry(ctx, func() error {
		var bedrockErr error
		if m.config.BedrockUseConverse {
			stream, bedrockErr = bedrockAdapter.CreateMessageStreamConverse(ctx, params)
		} else {
			stream, bedrockErr = bedrockAdapter.CreateMessageStream(ctx, params)
		}
		return bedrockErr
	}, nil)

	if err != nil {
		return m.bedrockStreamFailed(ctx, params, metadata, &bedrockCall{model: recordedModel, start: startTime, hasVision: hasVision, streamed: true}, err)
	}

	streamMetadata := buildStreamMetadata(metadata, recordedModel)
	wrapper := newStreamingWrapper(stream, m.config, streamMetadata, m, recordedModel, "AWS", hasVision, startTime)
	wrapper.captureRequest(params)

	inputTokens := estimateInputTokens(params)
	wrapper.SetInputTokens(inputTokens)

	return wrapper, nil
}

// bedrockCall is what the record of a failed Bedrock call needs; it is nil
// when the SDK could not set the call up. A call re-sent to the Anthropic API
// is recorded by that attempt instead.
type bedrockCall struct {
	model     string
	start     time.Time
	hasVision bool
	streamed  bool
}

// recordBedrockFailure skips a request the SDK refused to send.
func (m *MessagesInterface) recordBedrockFailure(call *bedrockCall, metadata map[string]interface{}, err error) {
	if call == nil || errors.Is(err, ErrBedrockCannotCarry) {
		return
	}
	timing := turnTiming{start: call.start, duration: time.Since(call.start), streamed: call.streamed}
	m.parent.metering.Send(buildFailedTurnPayload(turnUsage{model: call.model}, timing, "AWS", call.hasVision, metadata, err))
}

func (m *MessagesInterface) bedrockFailed(ctx context.Context, params anthropic.MessageNewParams, metadata map[string]interface{}, call *bedrockCall, err error) (*anthropic.Message, error) {
	if !m.config.BedrockFallbackToAnthropic {
		m.recordBedrockFailure(call, metadata, err)
		return nil, err
	}
	core.Warn("Bedrock request failed: %s, falling back to Anthropic", prompt.RedactedError(err))
	resp, fallbackErr := m.bedrockFallback(ctx, params, metadata)
	if fallbackNotSent(fallbackErr) {
		m.recordBedrockFailure(call, metadata, err)
	}
	return resp, fallbackErr
}

func (m *MessagesInterface) bedrockStreamFailed(ctx context.Context, params anthropic.MessageNewParams, metadata map[string]interface{}, call *bedrockCall, err error) (interface{}, error) {
	if !m.config.BedrockFallbackToAnthropic {
		m.recordBedrockFailure(call, metadata, err)
		return nil, err
	}
	core.Warn("Bedrock streaming request failed: %s, falling back to Anthropic", prompt.RedactedError(err))
	stream, fallbackErr := m.bedrockStreamingFallback(ctx, params, metadata)
	if fallbackNotSent(fallbackErr) {
		m.recordBedrockFailure(call, metadata, err)
	}
	return stream, fallbackErr
}

// errFallbackModel marks a Bedrock model id the fallback could not convert,
// so no Anthropic request was sent.
var errFallbackModel = errors.New("no Anthropic model for this Bedrock model")

// fallbackNotSent reports whether the fallback to the Anthropic API ended
// before sending its request, leaving the failed Bedrock call as the only
// one to record: the model could not be converted, or a cost control
// blocked the request.
func fallbackNotSent(err error) bool {
	var blocked *enforcement.ErrCostLimitExceeded
	return errors.Is(err, errFallbackModel) || errors.As(err, &blocked)
}

func (m *MessagesInterface) bedrockFallback(ctx context.Context, params anthropic.MessageNewParams, metadata map[string]interface{}) (*anthropic.Message, error) {
	fallbackParams := params
	convertedModel, err := ConvertBedrockARNToAnthropicModel(string(params.Model))
	if err != nil {
		return nil, fmt.Errorf("failed to convert Bedrock model for fallback: %w: %w", errFallbackModel, err)
	}
	fallbackParams.Model = anthropic.Model(convertedModel)
	core.Info("Converted Bedrock model '%s' to Anthropic model '%s' for fallback", params.Model, convertedModel)
	return m.createMessageAnthropic(ctx, fallbackParams, metadata)
}

func (m *MessagesInterface) bedrockStreamingFallback(ctx context.Context, params anthropic.MessageNewParams, metadata map[string]interface{}) (interface{}, error) {
	fallbackParams := params
	convertedModel, err := ConvertBedrockARNToAnthropicModel(string(params.Model))
	if err != nil {
		return nil, fmt.Errorf("failed to convert Bedrock model for streaming fallback: %w: %w", errFallbackModel, err)
	}
	fallbackParams.Model = anthropic.Model(convertedModel)
	core.Info("Converted Bedrock model '%s' to Anthropic model '%s' for streaming fallback", params.Model, convertedModel)
	return m.createMessageStreamAnthropic(ctx, fallbackParams, metadata)
}

func buildStreamMetadata(metadata map[string]interface{}, model string) map[string]interface{} {
	m := make(map[string]interface{})
	for k, v := range metadata {
		m[k] = v
	}
	if _, ok := m["model"]; !ok {
		m["model"] = model
	}
	return m
}

func Reset() {
	_ = sharedClient.ResetAndClose()
}
