package anthropic

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/revenium/revenium-go-sdk/core/metering"
	"github.com/revenium/revenium-go-sdk/core/resilience"
)

type ReveniumAnthropic struct {
	client   anthropic.Client
	config   *Config
	provider Provider
	mu       sync.RWMutex
	metering *metering.MeteringClient
}

var (
	globalClient *ReveniumAnthropic
	globalMu     sync.RWMutex
	initialized  bool
)

type AnthropicStatus struct {
	Initialized        bool
	HasConfig          bool
	Provider           string
	CircuitBreakerState string
}

func Initialize(opts ...Option) error {
	globalMu.Lock()
	defer globalMu.Unlock()

	if initialized {
		return nil
	}

	core.InitializeLogger()
	core.Info("Initializing Revenium middleware...")

	cfg := &Config{}
	for _, opt := range opts {
		opt(cfg)
	}
	if err := cfg.loadFromEnv(); err != nil {
		core.Warn("Failed to load configuration from environment: %v", err)
	}

	if err := cfg.Validate(); err != nil {
		return err
	}

	clientOpts := []option.RequestOption{}
	if cfg.AnthropicAPIKey != "" {
		clientOpts = append(clientOpts, option.WithAPIKey(cfg.AnthropicAPIKey))
	}

	anthropicClient := anthropic.NewClient(clientOpts...)
	provider := DetectProvider(cfg)

	mc, err := metering.NewMeteringClient(metering.MeteringClientConfig{
		APIKey:  cfg.Revenium.APIKey,
		BaseURL: cfg.Revenium.BaseURL,
	})
	if err != nil {
		return err
	}

	globalClient = &ReveniumAnthropic{
		client:   anthropicClient,
		config:   cfg,
		provider: provider,
		metering: mc,
	}

	initialized = true
	core.Info("Revenium middleware initialized successfully")
	return nil
}

func IsInitialized() bool {
	globalMu.RLock()
	defer globalMu.RUnlock()
	return initialized
}

func GetClient() (*ReveniumAnthropic, error) {
	globalMu.RLock()
	defer globalMu.RUnlock()

	if !initialized {
		return nil, core.NewConfigError("middleware not initialized, call Initialize() first", nil)
	}

	return globalClient, nil
}

func NewReveniumAnthropic(cfg *Config) (*ReveniumAnthropic, error) {
	if cfg == nil {
		return nil, core.NewConfigError("config cannot be nil", nil)
	}

	if cfg.Revenium == nil || cfg.Revenium.APIKey == "" {
		return nil, core.NewConfigError("REVENIUM_METERING_API_KEY is required", nil)
	}

	clientOpts := []option.RequestOption{}
	if cfg.AnthropicAPIKey != "" {
		clientOpts = append(clientOpts, option.WithAPIKey(cfg.AnthropicAPIKey))
	}

	anthropicClient := anthropic.NewClient(clientOpts...)
	provider := DetectProvider(cfg)

	mc, err := metering.NewMeteringClient(metering.MeteringClientConfig{
		APIKey:  cfg.Revenium.APIKey,
		BaseURL: cfg.Revenium.BaseURL,
	})
	if err != nil {
		return nil, err
	}

	return &ReveniumAnthropic{
		client:   anthropicClient,
		config:   cfg,
		provider: provider,
		metering: mc,
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

func (r *ReveniumAnthropic) GetAnthropicClient() anthropic.Client {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.client
}

func (r *ReveniumAnthropic) GetStatus() AnthropicStatus {
	r.mu.RLock()
	defer r.mu.RUnlock()

	cb := resilience.GetGlobalCircuitBreaker()

	return AnthropicStatus{
		Initialized:         IsInitialized(),
		HasConfig:           r.config != nil,
		Provider:            string(r.provider),
		CircuitBreakerState: cb.GetState().String(),
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

func (m *MessagesInterface) buildAnthropicPayload(resp *anthropic.Message, md map[string]interface{}, duration time.Duration, provider string, startTime time.Time, hasVision bool) *metering.MeteringPayload {
	normalizedProvider := NormalizeProviderName(provider)

	stopReason := "END"
	if resp.StopReason != "" {
		stopReason = MapStopReasonToRevenium(string(resp.StopReason))
	}

	totalTokens := resp.Usage.InputTokens + resp.Usage.OutputTokens

	payload := metering.NewPayload(metering.OperationChat, string(resp.Model), normalizedProvider).
		WithTiming(startTime, duration).
		WithTokens(resp.Usage.InputTokens, resp.Usage.OutputTokens, totalTokens).
		WithReasoningTokens(0, resp.Usage.CacheCreationInputTokens, resp.Usage.CacheReadInputTokens).
		WithStopReason(stopReason).
		Build()

	if hasVision {
		payload.Attributes = map[string]interface{}{"hasVision": true}
	}

	metering.ApplyMetadata(payload, md)
	return payload
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

	hasVision := DetectVisionContent(params.Messages)

	resp, err := m.client.Messages.New(ctx, params)
	if err != nil {
		return nil, err
	}

	duration := time.Since(startTime)
	payload := m.buildAnthropicPayload(resp, metadata, duration, "Anthropic", startTime, hasVision)
	m.parent.metering.Send(payload)

	return resp, nil
}

func (m *MessagesInterface) createMessageBedrock(ctx context.Context, params anthropic.MessageNewParams, metadata map[string]interface{}) (*anthropic.Message, error) {
	startTime := time.Now()

	bedrockAdapter, err := NewBedrockAdapter(m.config)
	if err != nil {
		core.Warn("Failed to create Bedrock adapter, falling back to Anthropic: %v", err)
		return m.bedrockFallback(ctx, params, metadata)
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
		core.Warn("Bedrock request failed after retries: %v, falling back to Anthropic", err)
		return m.bedrockFallback(ctx, params, metadata)
	}

	duration := time.Since(startTime)
	payload := m.buildAnthropicPayload(resp, metadata, duration, "AWS", startTime, hasVision)
	m.parent.metering.Send(payload)

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

	hasVision := DetectVisionContent(params.Messages)
	startTime := time.Now()
	stream := m.client.Messages.NewStreaming(ctx, params)

	streamMetadata := buildStreamMetadata(metadata, string(params.Model))
	wrapper := newStreamingWrapper(stream, m.config, streamMetadata, m, string(params.Model), "Anthropic", hasVision, startTime)

	inputTokens := estimateInputTokens(params)
	wrapper.SetInputTokens(inputTokens)

	return wrapper, nil
}

func (m *MessagesInterface) createMessageStreamBedrock(ctx context.Context, params anthropic.MessageNewParams, metadata map[string]interface{}) (interface{}, error) {
	bedrockAdapter, err := NewBedrockAdapter(m.config)
	if err != nil {
		core.Warn("Failed to create Bedrock adapter for streaming, falling back to Anthropic: %v", err)
		return m.bedrockStreamingFallback(ctx, params, metadata)
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
		core.Warn("Bedrock streaming failed after retries: %v, falling back to Anthropic", err)
		return m.bedrockStreamingFallback(ctx, params, metadata)
	}

	streamMetadata := buildStreamMetadata(metadata, string(params.Model))
	wrapper := newStreamingWrapper(stream, m.config, streamMetadata, m, string(params.Model), "AWS", hasVision, startTime)

	inputTokens := estimateInputTokens(params)
	wrapper.SetInputTokens(inputTokens)

	return wrapper, nil
}

func (m *MessagesInterface) bedrockFallback(ctx context.Context, params anthropic.MessageNewParams, metadata map[string]interface{}) (*anthropic.Message, error) {
	fallbackParams := params
	convertedModel, err := ConvertBedrockARNToAnthropicModel(string(params.Model))
	if err != nil {
		return nil, fmt.Errorf("failed to convert Bedrock model for fallback: %w", err)
	}
	fallbackParams.Model = anthropic.Model(convertedModel)
	core.Info("Converted Bedrock model '%s' to Anthropic model '%s' for fallback", params.Model, convertedModel)
	return m.createMessageAnthropic(ctx, fallbackParams, metadata)
}

func (m *MessagesInterface) bedrockStreamingFallback(ctx context.Context, params anthropic.MessageNewParams, metadata map[string]interface{}) (interface{}, error) {
	fallbackParams := params
	convertedModel, err := ConvertBedrockARNToAnthropicModel(string(params.Model))
	if err != nil {
		return nil, fmt.Errorf("failed to convert Bedrock model for streaming fallback: %w", err)
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
	globalMu.Lock()
	defer globalMu.Unlock()

	if globalClient != nil {
		globalClient.Close()
		globalClient = nil
	}

	initialized = false
}
