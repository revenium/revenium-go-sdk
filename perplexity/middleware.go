package perplexity

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/revenium/revenium-go-sdk/core/enforcement"
	"github.com/revenium/revenium-go-sdk/core/metering"
	"github.com/revenium/revenium-go-sdk/core/prompt"

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
	enforcer *enforcement.Engine
}

var sharedClient = core.NewSharedClient("NewReveniumPerplexity", func(c *ReveniumPerplexity) *core.ReveniumConfig { return c.config.Revenium })

func Initialize(opts ...Option) error {
	cfg := &Config{}
	for _, opt := range opts {
		opt(cfg)
	}
	return sharedClient.Initialize(core.NamedAccount(cfg.Revenium), func() (*ReveniumPerplexity, error) {
		return buildSharedClient(cfg)
	})
}

func buildSharedClient(cfg *Config) (*ReveniumPerplexity, error) {
	if err := cfg.loadFromEnv(); err != nil {
		core.Warn("Failed to load configuration from environment: %v", err)
	}

	core.Info("Initializing Revenium Perplexity middleware...")

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	mc, err := metering.NewMeteringClient(metering.ClientConfigFor(cfg.Revenium))
	if err != nil {
		return nil, err
	}

	clientOpts := buildPerplexityClientOptions(cfg)
	openaiClient := openai.NewClient(clientOpts...)

	candidate := &ReveniumPerplexity{
		client:   openaiClient,
		config:   cfg,
		metering: mc,
		enforcer: enforcement.StartFor(cfg.Revenium),
	}

	core.Info("Revenium Perplexity middleware initialized successfully")
	return candidate, nil
}

func IsInitialized() bool {
	return sharedClient.IsInitialized()
}

func GetClient() (*ReveniumPerplexity, error) {
	return sharedClient.Get()
}

func NewReveniumPerplexity(cfg *Config) (*ReveniumPerplexity, error) {
	if cfg == nil {
		return nil, core.NewConfigError("config cannot be nil", nil)
	}
	core.TrimReveniumConfig(cfg.Revenium)

	if cfg.Revenium == nil || cfg.Revenium.APIKey == "" {
		return nil, core.NewConfigError("REVENIUM_METERING_API_KEY is required", nil)
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	mc, err := metering.NewMeteringClient(metering.ClientConfigFor(cfg.Revenium))
	if err != nil {
		return nil, err
	}

	clientOpts := buildPerplexityClientOptions(cfg)
	openaiClient := openai.NewClient(clientOpts...)

	return &ReveniumPerplexity{
		client:   openaiClient,
		config:   cfg,
		metering: mc,
		enforcer: enforcement.StartFor(cfg.Revenium),
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

// GetOpenAIClient returns the provider's own client. Calls made through it are
// neither recorded nor checked against cost controls.
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
	r.enforcer.Release()
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

	if err := c.parent.enforcer.Check(enforcement.NewEvalContext(ctx, string(params.Model), "PERPLEXITY")); err != nil {
		core.Debug("Chat.Completions.New blocked by enforcement: %v", err)
		return nil, err
	}

	requestTime := time.Now()

	resp, err := c.client.Chat.Completions.New(ctx, params)
	if err != nil {
		duration := time.Since(requestTime)
		payload := metering.NewPayload(metering.OperationChat, string(params.Model), "PERPLEXITY").
			WithTiming(requestTime, duration).
			Build()
		metering.ApplyFailedCall(payload, callFailure(err), metadata)
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
	metering.ApplyChatCapture(payload, metadata, func() metering.ChatCapture {
		return metering.OpenAIChatCapture(params.Messages, chatReply(resp))
	})
	c.parent.metering.Send(payload)

	return resp, nil
}

func (c *CompletionsInterface) NewStreaming(ctx context.Context, params openai.ChatCompletionNewParams) (*StreamingWrapper, error) {
	metadata := core.ExtractMetadata(ctx, nil)

	if err := c.parent.enforcer.Check(enforcement.NewEvalContext(ctx, string(params.Model), "PERPLEXITY")); err != nil {
		core.Debug("Chat.Completions.NewStreaming blocked by enforcement: %v", err)
		return nil, err
	}

	stream := c.client.Chat.Completions.NewStreaming(ctx, params)

	streamMetadata := make(map[string]interface{})
	for k, v := range metadata {
		streamMetadata[k] = v
	}
	if _, ok := streamMetadata["model"]; !ok {
		streamMetadata["model"] = string(params.Model)
	}

	wrapper := &StreamingWrapper{
		stream:    stream,
		metadata:  streamMetadata,
		startTime: time.Now(),
		model:     string(params.Model),
		parent:    c.parent,
		params:    params,
		reply:     metering.ReplyCollectorFor(streamMetadata),
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
	closed         bool

	inputTokens  int64
	outputTokens int64
	totalTokens  int64

	finishReason string

	systemFingerprint string

	streamCost *PerplexityCost

	params openai.ChatCompletionNewParams
	reply  *prompt.ReplyCollector
}

// Next records each chunk exactly once, so usage, cost and the captured reply do not
// depend on whether, or how often, the caller reads Current.
func (sw *StreamingWrapper) Next() bool {
	if !sw.stream.Next() {
		return false
	}
	sw.recordChunk(sw.stream.Current())
	return true
}

func (sw *StreamingWrapper) Current() openai.ChatCompletionChunk {
	return sw.stream.Current()
}

func (sw *StreamingWrapper) recordChunk(chunk openai.ChatCompletionChunk) {
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

	if finishReason := choiceZero(chunk).FinishReason; finishReason != "" {
		sw.finishReason = finishReason
	}

	if chunk.SystemFingerprint != "" {
		sw.systemFingerprint = chunk.SystemFingerprint
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
	duration := time.Since(sw.startTime)

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
	if streamErr != nil {
		metering.ApplyFailedCall(payload, callFailure(streamErr), sw.metadata)
	} else {
		metering.ApplyMetadata(payload, sw.metadata)
		metering.ApplyChatCapture(payload, sw.metadata, func() metering.ChatCapture {
			return metering.OpenAIChatCapture(sw.params.Messages, "").WithStreamedReply(sw.reply)
		})
	}
	sw.parent.metering.Send(payload)

	return err
}

// chatReply, choiceZero, chunkReply and chunkFinished are copies of those in
// openai/prompt_capture.go and must change with them; providers may not import each other,
// and core cannot import the openai-go types they read.
func chatReply(resp *openai.ChatCompletion) string {
	if resp == nil || len(resp.Choices) == 0 {
		return ""
	}
	return resp.Choices[0].Message.Content
}

func choiceZero(chunk openai.ChatCompletionChunk) openai.ChatCompletionChunkChoice {
	for _, choice := range chunk.Choices {
		if choice.Index == 0 {
			return choice
		}
	}
	return openai.ChatCompletionChunkChoice{}
}

func chunkReply(chunk openai.ChatCompletionChunk) string {
	return choiceZero(chunk).Delta.Content
}

func chunkFinished(chunk openai.ChatCompletionChunk) bool {
	return choiceZero(chunk).FinishReason != ""
}
