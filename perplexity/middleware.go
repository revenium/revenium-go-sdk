package perplexity

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/revenium/revenium-go-sdk/core/metering"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/packages/ssestream"
)

type PerplexityCost struct {
	InputTokensCost  *float64 `json:"input_tokens_cost,omitempty"`
	OutputTokensCost *float64 `json:"output_tokens_cost,omitempty"`
	TotalCost        *float64 `json:"total_cost,omitempty"`
}

type ReveniumPerplexity struct {
	client   openai.Client
	config   *Config
	mu       sync.RWMutex
	metering *metering.MeteringClient
}

var (
	globalPerplexityClient *ReveniumPerplexity
	globalPerplexityMu     sync.RWMutex
	perplexityInitialized  bool
)

func Initialize(opts ...Option) error {
	globalPerplexityMu.Lock()
	defer globalPerplexityMu.Unlock()

	if perplexityInitialized {
		return nil
	}

	cfg := &Config{}
	for _, opt := range opts {
		opt(cfg)
	}

	if err := cfg.loadFromEnv(); err != nil {
		core.Warn("Failed to load configuration from environment: %v", err)
	}

	core.Info("Initializing Revenium Perplexity middleware...")

	if err := cfg.Validate(); err != nil {
		return err
	}

	mc, err := metering.NewMeteringClient(metering.MeteringClientConfig{
		APIKey:  cfg.Revenium.APIKey,
		BaseURL: cfg.Revenium.BaseURL,
	})
	if err != nil {
		return err
	}

	clientOpts := buildPerplexityClientOptions(cfg)
	openaiClient := openai.NewClient(clientOpts...)

	globalPerplexityClient = &ReveniumPerplexity{
		client:   openaiClient,
		config:   cfg,
		metering: mc,
	}

	perplexityInitialized = true
	core.Info("Revenium Perplexity middleware initialized successfully")
	return nil
}

func IsInitialized() bool {
	globalPerplexityMu.RLock()
	defer globalPerplexityMu.RUnlock()
	return perplexityInitialized
}

func GetClient() (*ReveniumPerplexity, error) {
	globalPerplexityMu.RLock()
	defer globalPerplexityMu.RUnlock()

	if !perplexityInitialized {
		return nil, core.NewConfigError("middleware not initialized, call Initialize() first", nil)
	}

	return globalPerplexityClient, nil
}

func NewReveniumPerplexity(cfg *Config) (*ReveniumPerplexity, error) {
	if cfg == nil {
		return nil, core.NewConfigError("config cannot be nil", nil)
	}

	if cfg.Revenium == nil || cfg.Revenium.APIKey == "" {
		return nil, core.NewConfigError("REVENIUM_METERING_API_KEY is required", nil)
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	mc, err := metering.NewMeteringClient(metering.MeteringClientConfig{
		APIKey:  cfg.Revenium.APIKey,
		BaseURL: cfg.Revenium.BaseURL,
	})
	if err != nil {
		return nil, err
	}

	clientOpts := buildPerplexityClientOptions(cfg)
	openaiClient := openai.NewClient(clientOpts...)

	return &ReveniumPerplexity{
		client:   openaiClient,
		config:   cfg,
		metering: mc,
	}, nil
}

func buildPerplexityClientOptions(cfg *Config) []option.RequestOption {
	clientOpts := []option.RequestOption{}

	if cfg.PerplexityAPIKey != "" {
		clientOpts = append(clientOpts, option.WithAPIKey(cfg.PerplexityAPIKey))
	}

	if cfg.PerplexityBaseURL != "" {
		clientOpts = append(clientOpts, option.WithBaseURL(cfg.PerplexityBaseURL))
	}

	return clientOpts
}

func (r *ReveniumPerplexity) GetConfig() *Config {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.config
}

func (r *ReveniumPerplexity) GetOpenAIClient() openai.Client {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.client
}

func (r *ReveniumPerplexity) Chat() *ChatInterface {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return &ChatInterface{
		client: r.client,
		config: r.config,
		parent: r,
	}
}

func (r *ReveniumPerplexity) Flush() {
	core.Debug("[Perplexity] Flushing pending metering requests...")
	r.metering.Flush()
	core.Debug("[Perplexity] All metering requests completed")
}

func (r *ReveniumPerplexity) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.metering.Close()
}

type ChatInterface struct {
	client openai.Client
	config *Config
	parent *ReveniumPerplexity
}

func (c *ChatInterface) Completions() *CompletionsInterface {
	return &CompletionsInterface{
		client: c.client,
		config: c.config,
		parent: c.parent,
	}
}

type CompletionsInterface struct {
	client openai.Client
	config *Config
	parent *ReveniumPerplexity
}

func (c *CompletionsInterface) New(ctx context.Context, params openai.ChatCompletionNewParams) (*openai.ChatCompletion, error) {
	metadata := core.ExtractMetadata(ctx, nil)
	requestTime := time.Now()

	resp, err := c.client.Chat.Completions.New(ctx, params)
	if err != nil {
		duration := time.Since(requestTime)
		payload := metering.NewPayload(metering.OperationChat, string(params.Model), "PERPLEXITY").
			WithTiming(requestTime, duration).
			WithError(err.Error()).
			Build()
		metering.ApplyMetadata(payload, metadata)
		c.parent.metering.Send(payload)
		return nil, err
	}

	duration := time.Since(requestTime)

	openaiFinishReason := ""
	if len(resp.Choices) > 0 {
		openaiFinishReason = resp.Choices[0].FinishReason
	}
	stopReason := string(MapOpenAIFinishReason(openaiFinishReason, StopReasonEnd))

	cost := extractPerplexityCost(resp)

	payload := metering.NewPayload(metering.OperationChat, string(resp.Model), "PERPLEXITY").
		WithTiming(requestTime, duration).
		WithTokens(resp.Usage.PromptTokens, resp.Usage.CompletionTokens, resp.Usage.TotalTokens).
		WithStopReason(stopReason).
		WithSystemFingerprint(resp.SystemFingerprint).
		Build()

	applyPerplexityCost(payload, cost)
	metering.ApplyMetadata(payload, metadata)
	c.parent.metering.Send(payload)

	return resp, nil
}

func (c *CompletionsInterface) NewStreaming(ctx context.Context, params openai.ChatCompletionNewParams) (*StreamingWrapper, error) {
	metadata := core.ExtractMetadata(ctx, nil)

	stream := c.client.Chat.Completions.NewStreaming(ctx, params)

	streamMetadata := make(map[string]interface{})
	for k, v := range metadata {
		streamMetadata[k] = v
	}
	if _, ok := streamMetadata["model"]; !ok {
		streamMetadata["model"] = string(params.Model)
	}

	wrapper := &StreamingWrapper{
		stream:   stream,
		metadata: streamMetadata,
		startTime: time.Now(),
		model:    string(params.Model),
		parent:   c.parent,
	}

	return wrapper, nil
}

func extractPerplexityCost(resp *openai.ChatCompletion) *PerplexityCost {
	rawJSON := resp.JSON.Usage.Raw()
	if rawJSON == "" {
		return nil
	}

	var usageWithCost struct {
		Cost *PerplexityCost `json:"cost,omitempty"`
	}

	if err := json.Unmarshal([]byte(rawJSON), &usageWithCost); err != nil {
		core.Debug("[Perplexity COST] Failed to parse cost from usage: %v", err)
		return nil
	}

	if usageWithCost.Cost != nil {
		core.Debug("[Perplexity COST] Extracted cost: input=%.6f, output=%.6f, total=%.6f",
			safeFloat(usageWithCost.Cost.InputTokensCost),
			safeFloat(usageWithCost.Cost.OutputTokensCost),
			safeFloat(usageWithCost.Cost.TotalCost))
	}

	return usageWithCost.Cost
}

func extractPerplexityCostFromChunk(chunk openai.ChatCompletionChunk) *PerplexityCost {
	rawJSON := chunk.Usage.RawJSON()
	if rawJSON == "" {
		return nil
	}

	var usageWithCost struct {
		Cost *PerplexityCost `json:"cost,omitempty"`
	}

	if err := json.Unmarshal([]byte(rawJSON), &usageWithCost); err != nil {
		core.Debug("[Perplexity COST] Failed to parse cost from streaming usage: %v", err)
		return nil
	}

	if usageWithCost.Cost != nil {
		core.Debug("[Perplexity COST] Extracted streaming cost: input=%.6f, output=%.6f, total=%.6f",
			safeFloat(usageWithCost.Cost.InputTokensCost),
			safeFloat(usageWithCost.Cost.OutputTokensCost),
			safeFloat(usageWithCost.Cost.TotalCost))
	}

	return usageWithCost.Cost
}

func safeFloat(f *float64) float64 {
	if f == nil {
		return 0
	}
	return *f
}

func applyPerplexityCost(payload *metering.MeteringPayload, cost *PerplexityCost) {
	if cost == nil {
		return
	}
	payload.InputTokenCost = cost.InputTokensCost
	payload.OutputTokenCost = cost.OutputTokensCost
	payload.TotalCost = cost.TotalCost
}

type StreamingWrapper struct {
	stream         *ssestream.Stream[openai.ChatCompletionChunk]
	metadata       map[string]interface{}
	startTime      time.Time
	firstTokenTime *time.Time
	model          string
	parent         *ReveniumPerplexity
	mu             sync.Mutex

	inputTokens  int64
	outputTokens int64
	totalTokens  int64

	finishReason string

	systemFingerprint string

	streamCost *PerplexityCost
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
	}

	if cost := extractPerplexityCostFromChunk(chunk); cost != nil {
		sw.streamCost = cost
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
		payload := metering.NewPayload(metering.OperationChat, sw.model, "PERPLEXITY").
			WithTiming(sw.startTime, duration).
			WithStreaming(true, 0, nil).
			WithError(streamErr.Error()).
			Build()
		metering.ApplyMetadata(payload, sw.metadata)
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
	stopReason := string(MapOpenAIFinishReason(finishReason, StopReasonEnd))

	payload := metering.NewPayload(metering.OperationChat, sw.model, "PERPLEXITY").
		WithTiming(sw.startTime, duration).
		WithTokens(sw.inputTokens, sw.outputTokens, sw.totalTokens).
		WithStreaming(true, timeToFirstToken, completionStartTime).
		WithStopReason(stopReason).
		WithSystemFingerprint(sw.systemFingerprint).
		Build()

	applyPerplexityCost(payload, sw.streamCost)
	metering.ApplyMetadata(payload, sw.metadata)
	sw.parent.metering.Send(payload)

	return err
}
