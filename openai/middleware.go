package openai

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/azure"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/packages/ssestream"
	"github.com/revenium/revenium-go-sdk/core"
	"github.com/revenium/revenium-go-sdk/core/metering"
)

type ReveniumOpenAI struct {
	client   openai.Client
	config   *Config
	provider Provider
	mu       sync.RWMutex
	metering *metering.MeteringClient
}

var (
	globalClient *ReveniumOpenAI
	globalMu     sync.RWMutex
	initialized  bool
)

func Initialize(opts ...Option) error {
	globalMu.Lock()
	defer globalMu.Unlock()

	if initialized {
		return nil
	}

	cfg := &Config{}
	for _, opt := range opts {
		opt(cfg)
	}

	if err := cfg.loadFromEnv(); err != nil {
		core.Warn("Failed to load configuration from environment: %v", err)
	}

	core.Info("Initializing Revenium middleware...")

	if err := cfg.Validate(); err != nil {
		return err
	}

	provider := DetectProvider(cfg)
	clientOpts := buildClientOptions(cfg, provider)
	openaiClient := openai.NewClient(clientOpts...)

	mc, err := metering.NewMeteringClient(metering.MeteringClientConfig{
		APIKey:  cfg.Revenium.APIKey,
		BaseURL: cfg.Revenium.BaseURL,
	})
	if err != nil {
		return err
	}

	globalClient = &ReveniumOpenAI{
		client:   openaiClient,
		config:   cfg,
		provider: provider,
		metering: mc,
	}

	initialized = true
	core.Info("Revenium middleware initialized successfully with provider: %s", provider)
	return nil
}

func IsInitialized() bool {
	globalMu.RLock()
	defer globalMu.RUnlock()
	return initialized
}

func GetClient() (*ReveniumOpenAI, error) {
	globalMu.RLock()
	defer globalMu.RUnlock()

	if !initialized {
		return nil, core.NewConfigError("middleware not initialized, call Initialize() first", nil)
	}

	return globalClient, nil
}

func NewReveniumOpenAI(cfg *Config) (*ReveniumOpenAI, error) {
	if cfg == nil {
		return nil, core.NewConfigError("config cannot be nil", nil)
	}

	if cfg.Revenium == nil || cfg.Revenium.APIKey == "" {
		return nil, core.NewConfigError("REVENIUM_METERING_API_KEY is required", nil)
	}

	provider := DetectProvider(cfg)
	clientOpts := buildClientOptions(cfg, provider)
	openaiClient := openai.NewClient(clientOpts...)

	mc, err := metering.NewMeteringClient(metering.MeteringClientConfig{
		APIKey:  cfg.Revenium.APIKey,
		BaseURL: cfg.Revenium.BaseURL,
	})
	if err != nil {
		return nil, err
	}

	return &ReveniumOpenAI{
		client:   openaiClient,
		config:   cfg,
		provider: provider,
		metering: mc,
	}, nil
}

func buildClientOptions(cfg *Config, provider Provider) []option.RequestOption {
	clientOpts := []option.RequestOption{}

	if provider == ProviderAzure {
		if cfg.AzureEndpoint != "" && cfg.AzureAPIVersion != "" {
			clientOpts = append(clientOpts, azure.WithEndpoint(cfg.AzureEndpoint, cfg.AzureAPIVersion))
			if cfg.AzureAPIKey != "" {
				clientOpts = append(clientOpts, azure.WithAPIKey(cfg.AzureAPIKey))
			}
			core.Info("Configured Azure OpenAI with endpoint: %s, API version: %s", cfg.AzureEndpoint, cfg.AzureAPIVersion)
		}
	} else {
		if cfg.OpenAIAPIKey != "" {
			clientOpts = append(clientOpts, option.WithAPIKey(cfg.OpenAIAPIKey))
		}
		if cfg.OpenAIOrgID != "" {
			clientOpts = append(clientOpts, option.WithOrganization(cfg.OpenAIOrgID))
		}
		if cfg.BaseURL != "" {
			clientOpts = append(clientOpts, option.WithBaseURL(cfg.BaseURL))
		}
	}

	return clientOpts
}

func (r *ReveniumOpenAI) GetConfig() *Config {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.config
}

func (r *ReveniumOpenAI) GetProvider() Provider {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.provider
}

func (r *ReveniumOpenAI) GetOpenAIClient() openai.Client {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.client
}

func (r *ReveniumOpenAI) Chat() *ChatInterface {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return &ChatInterface{
		client:   r.client,
		config:   r.config,
		provider: r.provider,
		parent:   r,
	}
}

func (r *ReveniumOpenAI) Embeddings() *EmbeddingsInterface {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return &EmbeddingsInterface{
		client:   r.client,
		config:   r.config,
		provider: r.provider,
		parent:   r,
	}
}

func (r *ReveniumOpenAI) Images() *ImagesInterface {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return &ImagesInterface{
		client:   r.client,
		config:   r.config,
		provider: r.provider,
		parent:   r,
	}
}

func (r *ReveniumOpenAI) Audio() *AudioInterface {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return &AudioInterface{
		client:   r.client,
		config:   r.config,
		provider: r.provider,
		parent:   r,
	}
}

func (r *ReveniumOpenAI) Responses() *ResponsesInterface {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return &ResponsesInterface{
		client:   r.client.Responses,
		config:   r.config,
		provider: r.provider,
		parent:   r,
	}
}

func (r *ReveniumOpenAI) Flush() {
	r.metering.Flush()
}

func (r *ReveniumOpenAI) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.metering.Close()
}

type ChatInterface struct {
	client   openai.Client
	config   *Config
	provider Provider
	parent   *ReveniumOpenAI
}

func (c *ChatInterface) Completions() *CompletionsInterface {
	return &CompletionsInterface{
		client:   c.client,
		config:   c.config,
		provider: c.provider,
		parent:   c.parent,
	}
}

type CompletionsInterface struct {
	client   openai.Client
	config   *Config
	provider Provider
	parent   *ReveniumOpenAI
}

func (c *CompletionsInterface) New(ctx context.Context, params openai.ChatCompletionNewParams) (*openai.ChatCompletion, error) {
	metadata := core.GetUsageMetadata(ctx)

	switch c.provider {
	case ProviderOpenAI:
		return c.createCompletionOpenAI(ctx, params, metadata)
	case ProviderAzure:
		return c.createCompletionAzure(ctx, params, metadata)
	default:
		return nil, core.NewProviderError("unknown provider", fmt.Errorf("provider: %v", c.provider))
	}
}

func (c *CompletionsInterface) NewStreaming(ctx context.Context, params openai.ChatCompletionNewParams) (*StreamingWrapper, error) {
	metadata := core.GetUsageMetadata(ctx)

	switch c.provider {
	case ProviderOpenAI:
		return c.createCompletionStreamingOpenAI(ctx, params, metadata)
	case ProviderAzure:
		return c.createCompletionStreamingAzure(ctx, params, metadata)
	default:
		return nil, core.NewProviderError("unknown provider", fmt.Errorf("provider: %v", c.provider))
	}
}

func (c *CompletionsInterface) buildResponsePayload(resp *openai.ChatCompletion, md map[string]interface{}, isStreamed bool, duration time.Duration, provider string, requestTime time.Time, completionStartTime *time.Time, timeToFirstToken int64) *metering.MeteringPayload {
	if provider == "" {
		provider = "OPENAI"
	}

	reasoningTokens := resp.Usage.CompletionTokensDetails.ReasoningTokens
	cacheReadTokens := resp.Usage.PromptTokensDetails.CachedTokens

	openaiFinishReason := ""
	if len(resp.Choices) > 0 {
		openaiFinishReason = resp.Choices[0].FinishReason
	}
	stopReason := string(MapOpenAIFinishReason(openaiFinishReason, core.StopReasonEnd))

	payload := metering.NewPayload(metering.OperationChat, string(resp.Model), provider).
		WithTiming(requestTime, duration).
		WithTokens(resp.Usage.PromptTokens, resp.Usage.CompletionTokens, resp.Usage.TotalTokens).
		WithReasoningTokens(reasoningTokens, 0, cacheReadTokens).
		WithStreaming(isStreamed, timeToFirstToken, completionStartTime).
		WithStopReason(stopReason).
		WithSystemFingerprint(resp.SystemFingerprint).
		Build()

	metering.ApplyMetadata(payload, md)
	return payload
}

func (c *CompletionsInterface) buildErrorPayload(model string, md map[string]interface{}, isStreamed bool, duration time.Duration, provider string, requestTime time.Time, errorReason string) *metering.MeteringPayload {
	if provider == "" {
		provider = "OPENAI"
	}

	payload := metering.NewPayload(metering.OperationChat, model, provider).
		WithTiming(requestTime, duration).
		WithStreaming(isStreamed, 0, nil).
		WithError(errorReason).
		Build()

	metering.ApplyMetadata(payload, md)
	return payload
}

func (c *CompletionsInterface) createCompletionOpenAI(ctx context.Context, params openai.ChatCompletionNewParams, metadata map[string]interface{}) (*openai.ChatCompletion, error) {
	requestTime := time.Now()

	resp, err := c.client.Chat.Completions.New(ctx, params)
	if err != nil {
		duration := time.Since(requestTime)
		payload := c.buildErrorPayload(string(params.Model), metadata, false, duration, "OPENAI", requestTime, err.Error())
		c.parent.metering.Send(payload)
		return nil, err
	}

	duration := time.Since(requestTime)
	payload := c.buildResponsePayload(resp, metadata, false, duration, "OPENAI", requestTime, nil, 0)
	c.parent.metering.Send(payload)

	return resp, nil
}

func (c *CompletionsInterface) createCompletionAzure(ctx context.Context, params openai.ChatCompletionNewParams, metadata map[string]interface{}) (*openai.ChatCompletion, error) {
	requestTime := time.Now()
	originalModel := string(params.Model)
	core.Debug("Using Azure deployment name '%s' from user", originalModel)

	resp, err := c.client.Chat.Completions.New(ctx, params)
	if err != nil {
		core.Warn("Azure request failed: %v, falling back to OpenAI", err)
		duration := time.Since(requestTime)
		payload := c.buildErrorPayload(originalModel, metadata, false, duration, "AZURE", requestTime, err.Error())
		c.parent.metering.Send(payload)
		return c.createCompletionOpenAI(ctx, params, metadata)
	}

	duration := time.Since(requestTime)
	payload := c.buildResponsePayload(resp, metadata, false, duration, "AZURE", requestTime, nil, 0)
	c.parent.metering.Send(payload)

	return resp, nil
}

func (c *CompletionsInterface) createCompletionStreamingOpenAI(ctx context.Context, params openai.ChatCompletionNewParams, metadata map[string]interface{}) (*StreamingWrapper, error) {
	stream := c.client.Chat.Completions.NewStreaming(ctx, params)

	streamMetadata := make(map[string]interface{})
	if metadata != nil {
		for k, v := range metadata {
			streamMetadata[k] = v
		}
	}

	if _, ok := streamMetadata["model"]; !ok {
		streamMetadata["model"] = string(params.Model)
	}

	return &StreamingWrapper{
		stream:      stream,
		config:      c.config,
		metadata:    streamMetadata,
		startTime:   time.Now(),
		completions: c,
		model:       string(params.Model),
		provider:    "OPENAI",
		parent:      c.parent,
	}, nil
}

func (c *CompletionsInterface) createCompletionStreamingAzure(ctx context.Context, params openai.ChatCompletionNewParams, metadata map[string]interface{}) (*StreamingWrapper, error) {
	originalModel := string(params.Model)
	core.Debug("Using Azure deployment name '%s' from user", originalModel)

	stream := c.client.Chat.Completions.NewStreaming(ctx, params)

	streamMetadata := make(map[string]interface{})
	if metadata != nil {
		for k, v := range metadata {
			streamMetadata[k] = v
		}
	}

	if _, ok := streamMetadata["model"]; !ok {
		streamMetadata["model"] = originalModel
	}

	return &StreamingWrapper{
		stream:      stream,
		config:      c.config,
		metadata:    streamMetadata,
		startTime:   time.Now(),
		completions: c,
		model:       originalModel,
		provider:    "AZURE",
		parent:      c.parent,
	}, nil
}

type StreamingWrapper struct {
	stream         *ssestream.Stream[openai.ChatCompletionChunk]
	config         *Config
	metadata       map[string]interface{}
	startTime      time.Time
	firstTokenTime *time.Time
	completions    *CompletionsInterface
	model          string
	provider       string
	parent         *ReveniumOpenAI
	mu             sync.Mutex

	inputTokens         int64
	outputTokens        int64
	totalTokens         int64
	reasoningTokens     int64
	cacheReadTokens     int64
	cacheCreationTokens int64

	finishReason      string
	systemFingerprint string
}

func (sw *StreamingWrapper) Next() bool {
	return sw.stream.Next()
}

func (sw *StreamingWrapper) Current() openai.ChatCompletionChunk {
	chunk := sw.stream.Current()

	sw.mu.Lock()
	defer sw.mu.Unlock()

	if sw.firstTokenTime == nil && len(chunk.Choices) > 0 {
		now := time.Now()
		sw.firstTokenTime = &now
	}

	if chunk.Usage.PromptTokens > 0 || chunk.Usage.CompletionTokens > 0 {
		sw.inputTokens = chunk.Usage.PromptTokens
		sw.outputTokens = chunk.Usage.CompletionTokens
		sw.totalTokens = chunk.Usage.TotalTokens

		if chunk.Usage.CompletionTokensDetails.ReasoningTokens > 0 {
			sw.reasoningTokens = chunk.Usage.CompletionTokensDetails.ReasoningTokens
		}

		if chunk.Usage.PromptTokensDetails.CachedTokens > 0 {
			sw.cacheReadTokens = chunk.Usage.PromptTokensDetails.CachedTokens
		}
	}

	if len(chunk.Choices) > 0 && chunk.Choices[0].FinishReason != "" {
		sw.finishReason = chunk.Choices[0].FinishReason
	}

	if chunk.SystemFingerprint != "" {
		sw.systemFingerprint = chunk.SystemFingerprint
	}

	return chunk
}

func (sw *StreamingWrapper) Err() error {
	return sw.stream.Err()
}

func (sw *StreamingWrapper) Close() error {
	err := sw.stream.Close()
	streamErr := sw.stream.Err()
	duration := time.Since(sw.startTime)

	sw.mu.Lock()
	defer sw.mu.Unlock()

	if streamErr != nil {
		payload := sw.completions.buildErrorPayload(sw.model, sw.metadata, true, duration, sw.provider, sw.startTime, streamErr.Error())
		sw.parent.metering.Send(payload)
		return err
	}

	timeToFirstToken := int64(0)
	var completionStartTime *time.Time
	if sw.firstTokenTime != nil {
		timeToFirstToken = sw.firstTokenTime.Sub(sw.startTime).Milliseconds()
		completionStartTime = sw.firstTokenTime
	}

	finishReason := sw.finishReason
	if finishReason == "" {
		finishReason = "stop"
	}

	stopReason := string(MapOpenAIFinishReason(finishReason, core.StopReasonEnd))

	payload := metering.NewPayload(metering.OperationChat, sw.model, sw.provider).
		WithTiming(sw.startTime, duration).
		WithTokens(sw.inputTokens, sw.outputTokens, sw.totalTokens).
		WithReasoningTokens(sw.reasoningTokens, sw.cacheCreationTokens, sw.cacheReadTokens).
		WithStreaming(true, timeToFirstToken, completionStartTime).
		WithStopReason(stopReason).
		WithSystemFingerprint(sw.systemFingerprint).
		Build()

	metering.ApplyMetadata(payload, sw.metadata)
	sw.parent.metering.Send(payload)

	return err
}
