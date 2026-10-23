package openai

import (
	"context"
	"sync"
	"time"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/azure"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/packages/ssestream"
	"github.com/revenium/revenium-go-sdk/core"
	"github.com/revenium/revenium-go-sdk/core/enforcement"
	"github.com/revenium/revenium-go-sdk/core/metering"
	"github.com/revenium/revenium-go-sdk/core/prompt"
)

type ReveniumOpenAI struct {
	client   openai.Client
	config   *Config
	provider Provider
	mu       sync.RWMutex
	metering *metering.MeteringClient
	enforcer *enforcement.Engine
}

var sharedClient = core.NewSharedClient("NewReveniumOpenAI", func(c *ReveniumOpenAI) *core.ReveniumConfig { return c.config.Revenium })

// Initialize sets up the shared client for one Revenium account. A repeat
// call, including one made while the first setup is still running, returns
// nil when it names the same Revenium key, base URL and team, or none of
// them, and a config error otherwise; use NewReveniumOpenAI for several
// accounts in one process.
func Initialize(opts ...Option) error {
	cfg := &Config{}
	for _, opt := range opts {
		opt(cfg)
	}
	return sharedClient.Initialize(core.NamedAccount(cfg.Revenium), func() (*ReveniumOpenAI, error) {
		return buildSharedClient(cfg)
	})
}

func buildSharedClient(cfg *Config) (*ReveniumOpenAI, error) {
	if err := cfg.loadFromEnv(); err != nil {
		core.Warn("Failed to load configuration from environment: %v", err)
	}

	core.Info("Initializing Revenium middleware...")

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	provider, openaiClient, err := newProviderClient(cfg)
	if err != nil {
		return nil, err
	}

	mc, err := metering.NewMeteringClient(metering.ClientConfigFor(cfg.Revenium))
	if err != nil {
		return nil, err
	}

	candidate := &ReveniumOpenAI{
		client:   openaiClient,
		config:   cfg,
		provider: provider,
		metering: mc,
		enforcer: enforcement.StartFor(cfg.Revenium),
	}

	core.Info("Revenium middleware initialized successfully with provider: %s", provider)
	return candidate, nil
}

func IsInitialized() bool {
	return sharedClient.IsInitialized()
}

func GetClient() (*ReveniumOpenAI, error) {
	return sharedClient.Get()
}

func NewReveniumOpenAI(cfg *Config) (*ReveniumOpenAI, error) {
	if cfg == nil {
		return nil, core.NewConfigError("config cannot be nil", nil)
	}
	core.TrimReveniumConfig(cfg.Revenium)

	if cfg.Revenium == nil || cfg.Revenium.APIKey == "" {
		return nil, core.NewConfigError("REVENIUM_METERING_API_KEY is required", nil)
	}

	provider, openaiClient, err := newProviderClient(cfg)
	if err != nil {
		return nil, err
	}

	mc, err := metering.NewMeteringClient(metering.ClientConfigFor(cfg.Revenium))
	if err != nil {
		return nil, err
	}

	return &ReveniumOpenAI{
		client:   openaiClient,
		config:   cfg,
		provider: provider,
		metering: mc,
		enforcer: enforcement.StartFor(cfg.Revenium),
	}, nil
}

func newProviderClient(cfg *Config) (Provider, openai.Client, error) {
	provider := DetectProvider(cfg)
	if err := cfg.validateAzure(provider); err != nil {
		return provider, openai.Client{}, err
	}
	return provider, openai.NewClient(buildClientOptions(cfg, provider)...), nil
}

func buildClientOptions(cfg *Config, provider Provider) []option.RequestOption {
	if provider.IsAzure() {
		return azureClientOptions(cfg)
	}
	return openAIClientOptions(cfg)
}

func azureClientOptions(cfg *Config) []option.RequestOption {
	endpoint := cfg.azureEndpoint()
	if isAzureV1Endpoint(endpoint) {
		return azureV1ClientOptions(cfg, endpoint)
	}
	endpoint = azureResourceEndpoint(endpoint)
	version := cfg.azureAPIVersion()
	core.Info("Configured Azure OpenAI with endpoint: %s, API version: %s", endpoint, version)
	return []option.RequestOption{
		azure.WithEndpoint(endpoint, version),
		azure.WithAPIKey(cfg.AzureAPIKey),
	}
}

// azureV1ClientOptions sends the Azure key as a Bearer API key, as Azure's v1
// setup for the OpenAI client documents, and clears OPENAI_* organization, project
// and admin-key values the client would otherwise read from the environment.
func azureV1ClientOptions(cfg *Config, endpoint string) []option.RequestOption {
	core.Info("Configured Azure OpenAI v1 API with endpoint: %s", endpoint)
	opts := []option.RequestOption{
		option.WithBaseURL(endpoint),
		option.WithAdminAPIKey(""),
		option.WithAPIKey(cfg.AzureAPIKey),
		option.WithHeaderDel("OpenAI-Organization"),
		option.WithHeaderDel("OpenAI-Project"),
	}
	if cfg.AzureAPIVersion != "" {
		opts = append(opts, option.WithQueryAdd("api-version", cfg.AzureAPIVersion))
	}
	return opts
}

func openAIClientOptions(cfg *Config) []option.RequestOption {
	clientOpts := []option.RequestOption{}
	if cfg.OpenAIAPIKey != "" {
		clientOpts = append(clientOpts, option.WithAPIKey(cfg.OpenAIAPIKey))
	}
	if cfg.OpenAIOrgID != "" {
		clientOpts = append(clientOpts, option.WithOrganization(cfg.OpenAIOrgID))
	}
	if cfg.BaseURL != "" {
		clientOpts = append(clientOpts, option.WithBaseURL(cfg.BaseURL))
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

// GetOpenAIClient returns the provider's own client. Calls made through it are
// neither recorded nor checked against cost controls.
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
	r.enforcer.Release()
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

	if err := c.parent.checkCostControls(ctx, string(params.Model), c.provider, c.provider.recordsServedChatModel()); err != nil {
		core.Debug("Chat.Completions.New blocked by enforcement: %v", err)
		return nil, err
	}

	return c.createCompletion(ctx, params, metadata)
}

func (c *CompletionsInterface) NewStreaming(ctx context.Context, params openai.ChatCompletionNewParams) (*StreamingWrapper, error) {
	metadata := core.GetUsageMetadata(ctx)

	if err := c.parent.checkCostControls(ctx, string(params.Model), c.provider, c.provider.recordsServedChatModel()); err != nil {
		core.Debug("Chat.Completions.NewStreaming blocked by enforcement: %v", err)
		return nil, err
	}

	return c.startStreaming(ctx, params, metadata, c.provider), nil
}

func (c *CompletionsInterface) buildResponsePayload(resp *openai.ChatCompletion, model string, md map[string]interface{}, isStreamed bool, duration time.Duration, provider Provider, requestTime time.Time, completionStartTime *time.Time, timeToFirstToken int64) *metering.MeteringPayload {
	openaiFinishReason := ""
	if len(resp.Choices) > 0 {
		openaiFinishReason = resp.Choices[0].FinishReason
	}

	payload := completionUsagePayload(model, provider, resp.Usage, openaiFinishReason, resp.SystemFingerprint).
		WithTiming(requestTime, duration).
		WithStreaming(isStreamed, timeToFirstToken, completionStartTime).
		Build()

	metering.ApplyMetadata(payload, md)
	return payload
}

func completionUsagePayload(model string, provider Provider, usage openai.CompletionUsage, finishReason, systemFingerprint string) *metering.PayloadBuilder {
	stopReason := string(MapOpenAIFinishReason(finishReason, core.StopReasonEnd))
	return chatTokenUsage(usage).chatRecord(provider, model).
		WithStopReason(stopReason).
		WithSystemFingerprint(systemFingerprint)
}

func (c *CompletionsInterface) createCompletion(ctx context.Context, params openai.ChatCompletionNewParams, metadata map[string]interface{}) (*openai.ChatCompletion, error) {
	requestTime := time.Now()
	model := string(params.Model)
	provider := c.provider

	resp, err := c.client.Chat.Completions.New(ctx, params)
	duration := time.Since(requestTime)
	if err != nil {
		c.parent.metering.Send(chatFailureRecord(provider, model, metadata, false, requestTime, duration, err))
		return nil, err
	}

	payload := c.buildResponsePayload(resp, provider.chatRecordModel(model, resp.Model), metadata, false, duration, provider, requestTime, nil, 0)
	metering.ApplyChatCapture(payload, metadata, func() metering.ChatCapture { return chatCapture(params, chatReply(resp)) })
	c.parent.metering.Send(payload)
	return resp, nil
}

func (c *CompletionsInterface) startStreaming(ctx context.Context, params openai.ChatCompletionNewParams, metadata map[string]interface{}, provider Provider) *StreamingWrapper {
	model := string(params.Model)
	streamOptions, callerRequestedUsage := withUsageReporting(params.StreamOptions)
	params.StreamOptions = streamOptions
	clock := startStreamClock()
	stream := c.client.Chat.Completions.NewStreaming(ctx, params)

	streamMetadata := make(map[string]interface{}, len(metadata)+1)
	for k, v := range metadata {
		streamMetadata[k] = v
	}
	if _, ok := streamMetadata["model"]; !ok {
		streamMetadata["model"] = model
	}

	return &StreamingWrapper{
		stream:         stream,
		config:         c.config,
		metadata:       streamMetadata,
		clock:          clock,
		completions:    c,
		model:          model,
		provider:       provider,
		parent:         c.parent,
		hideUsageChunk: !callerRequestedUsage,
		params:         params,
		reply:          metering.ReplyCollectorFor(metadata),
	}
}

func withUsageReporting(options openai.ChatCompletionStreamOptionsParam) (openai.ChatCompletionStreamOptionsParam, bool) {
	callerRequestedUsage := options.IncludeUsage.Valid() && options.IncludeUsage.Value
	options.IncludeUsage = openai.Bool(true)
	return options, callerRequestedUsage
}

type StreamingWrapper struct {
	stream         *ssestream.Stream[openai.ChatCompletionChunk]
	config         *Config
	metadata       map[string]interface{}
	clock          streamClock
	completions    *CompletionsInterface
	model          string
	provider       Provider
	parent         *ReveniumOpenAI
	hideUsageChunk bool
	mu             sync.Mutex
	closed         bool

	params openai.ChatCompletionNewParams
	reply  *prompt.ReplyCollector

	usage tokenUsage

	finishReason      string
	systemFingerprint string
	servedModel       string
}

func (sw *StreamingWrapper) Next() bool {
	for sw.stream.Next() {
		chunk := sw.stream.Current()
		sw.recordChunk(chunk)
		if !sw.isHiddenUsageChunk(chunk) {
			return true
		}
	}
	sw.markExhausted()
	return false
}

func (sw *StreamingWrapper) markExhausted() {
	sw.mu.Lock()
	defer sw.mu.Unlock()
	sw.clock.markExhausted()
}

func (sw *StreamingWrapper) Current() openai.ChatCompletionChunk {
	return sw.stream.Current()
}

func (sw *StreamingWrapper) isHiddenUsageChunk(chunk openai.ChatCompletionChunk) bool {
	return sw.hideUsageChunk && len(chunk.Choices) == 0 && chunk.JSON.Usage.Valid()
}

func (sw *StreamingWrapper) recordChunk(chunk openai.ChatCompletionChunk) {
	sw.mu.Lock()
	defer sw.mu.Unlock()

	sw.clock.observe(chatChunkHasOutput(chunk), chatChoiceProgress(chunk))

	if chunk.Usage.PromptTokens > 0 || chunk.Usage.CompletionTokens > 0 {
		sw.usage = chatTokenUsage(chunk.Usage)
	}

	if finishReason := choiceZero(chunk).FinishReason; finishReason != "" {
		sw.finishReason = finishReason
	}

	if chunk.SystemFingerprint != "" {
		sw.systemFingerprint = chunk.SystemFingerprint
	}

	if sw.servedModel == "" {
		sw.servedModel = chunk.Model
	}

	sw.reply.Add(chunkReply(chunk))
	if chunkFinished(chunk) {
		sw.reply.End()
	}
}

func (sw *StreamingWrapper) Err() error {
	return sw.stream.Err()
}

func (sw *StreamingWrapper) Close() error {
	sw.mu.Lock()
	defer sw.mu.Unlock()

	if sw.closed {
		return nil
	}
	sw.closed = true

	err := sw.stream.Close()
	streamErr := sw.stream.Err()

	finishReason := sw.finishReason
	if finishReason == "" {
		finishReason = "stop"
	}

	stopReason := string(MapOpenAIFinishReason(finishReason, core.StopReasonEnd))

	payload := sw.usage.chatRecord(sw.provider, sw.provider.chatRecordModel(sw.model, sw.servedModel)).
		WithTiming(sw.clock.start, sw.clock.duration()).
		WithStreaming(true, sw.clock.timeToFirstToken(), sw.clock.firstToken).
		WithStopReason(stopReason).
		WithSystemFingerprint(sw.systemFingerprint).
		Build()

	applyStreamOutcome(payload, streamErr, sw.metadata, func() metering.ChatCapture { return chatCapture(sw.params, "").WithStreamedReply(sw.reply) })
	sw.parent.metering.Send(payload)

	return err
}
