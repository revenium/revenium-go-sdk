package google

import (
	"context"
	"errors"
	"iter"
	"net/http"
	"sync"
	"time"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/revenium/revenium-go-sdk/core/enforcement"
	"github.com/revenium/revenium-go-sdk/core/metering"
	"google.golang.org/genai"
)

type ReveniumGoogle struct {
	client   *genai.Client
	config   *Config
	provider Provider
	mu       sync.RWMutex
	metering *metering.MeteringClient
	videos   *videoTracker
	enforcer *enforcement.Engine
}

var sharedClient = core.NewSharedClient("NewReveniumGoogle", func(c *ReveniumGoogle) *core.ReveniumConfig { return c.config.Revenium })

func createGenaiClient(ctx context.Context, cfg *Config, provider Provider) (*genai.Client, error) {
	if provider.IsVertexAI() {
		if cfg.ProjectID == "" {
			return nil, core.NewConfigError("GOOGLE_CLOUD_PROJECT is required for Vertex AI", nil)
		}
		if cfg.Location == "" {
			return nil, core.NewConfigError("GOOGLE_CLOUD_LOCATION is required for Vertex AI", nil)
		}
		return genai.NewClient(ctx, &genai.ClientConfig{
			Project:  cfg.ProjectID,
			Location: cfg.Location,
			Backend:  genai.BackendVertexAI,
		})
	}

	if cfg.GoogleAPIKey == "" {
		return nil, core.NewConfigError("GOOGLE_API_KEY is required for Google AI", nil)
	}
	return genai.NewClient(ctx, &genai.ClientConfig{
		APIKey:  cfg.GoogleAPIKey,
		Backend: genai.BackendGeminiAPI,
	})
}

func newMeteringClient(cfg *Config) (*metering.MeteringClient, error) {
	return metering.NewMeteringClient(metering.ClientConfigFor(cfg.Revenium))
}

func assembleReveniumGoogle(ctx context.Context, cfg *Config, provider Provider, genaiClient *genai.Client, mc *metering.MeteringClient) (*ReveniumGoogle, error) {
	videos := newVideoTracker(provider, mc.Send)
	trackedClient, err := withRequestTracking(ctx, genaiClient, videos)
	if err != nil {
		return nil, errors.Join(core.NewProviderError("failed to create Google Genai client", err), mc.Close())
	}
	return &ReveniumGoogle{
		client:   trackedClient,
		config:   cfg,
		provider: provider,
		metering: mc,
		videos:   videos,
		enforcer: enforcement.StartFor(cfg.Revenium),
	}, nil
}

// withRequestTracking rebuilds the genai client around a copy of its resolved HTTP
// client with two transports, because genai exposes no hook on either:
// sendingTransport notes that a call's request was sent, which every Google call
// reads before recording a failure, so a call genai refuses locally is not
// recorded; videoOperationTransport records Veo operations polled through
// Operations.GetVideosOperation, which never passes through this package. The copy
// keeps the original transport, so Vertex credentials still apply.
func withRequestTracking(ctx context.Context, client *genai.Client, videos *videoTracker) (*genai.Client, error) {
	config := client.ClientConfig()
	tracked := &http.Client{}
	if config.HTTPClient != nil {
		copied := *config.HTTPClient
		tracked = &copied
	}
	base := tracked.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	tracked.Transport = &videoOperationTransport{base: &sendingTransport{base: base}, tracker: videos}
	config.HTTPClient = tracked
	return genai.NewClient(ctx, &config)
}

// Initialize sets up the shared client for one Revenium account. A repeat
// call, including one made while the first setup is still running, returns
// nil when it names the same Revenium key, base URL and team, or none of
// them, and a config error otherwise; use NewReveniumGoogle for several
// accounts in one process.
func Initialize(opts ...Option) error {
	cfg := &Config{}
	for _, opt := range opts {
		opt(cfg)
	}
	return sharedClient.Initialize(core.NamedAccount(cfg.Revenium), func() (*ReveniumGoogle, error) {
		return buildSharedClient(cfg)
	})
}

func buildSharedClient(cfg *Config) (*ReveniumGoogle, error) {
	core.InitializeLogger()
	core.Info("Initializing Revenium middleware...")

	if err := cfg.loadFromEnv(); err != nil {
		core.Warn("Failed to load configuration from environment: %v", err)
	}

	if cfg.Revenium != nil {
		core.EnableDebugWhenRequested(cfg.Revenium.Debug)
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	provider := DetectProvider(cfg)

	ctx := context.Background()
	genaiClient, err := createGenaiClient(ctx, cfg, provider)
	if err != nil {
		return nil, core.NewProviderError("failed to create Google Genai client", err)
	}

	mc, err := newMeteringClient(cfg)
	if err != nil {
		return nil, err
	}

	candidate, err := assembleReveniumGoogle(ctx, cfg, provider, genaiClient, mc)
	if err != nil {
		return nil, err
	}

	core.Info("Revenium middleware initialized successfully with provider: %s", provider.String())
	return candidate, nil
}

func IsInitialized() bool {
	return sharedClient.IsInitialized()
}

func GetClient() (*ReveniumGoogle, error) {
	return sharedClient.Get()
}

func NewReveniumGoogle(cfg *Config) (*ReveniumGoogle, error) {
	if cfg == nil {
		return nil, core.NewConfigError("config cannot be nil", nil)
	}
	core.TrimReveniumConfig(cfg.Revenium)

	if cfg.Revenium == nil || cfg.Revenium.APIKey == "" {
		return nil, core.NewConfigError("REVENIUM_METERING_API_KEY is required", nil)
	}

	provider := DetectProvider(cfg)

	ctx := context.Background()
	genaiClient, err := createGenaiClient(ctx, cfg, provider)
	if err != nil {
		return nil, core.NewProviderError("failed to create Google Genai client", err)
	}

	mc, err := newMeteringClient(cfg)
	if err != nil {
		return nil, err
	}

	client, err := assembleReveniumGoogle(ctx, cfg, provider, genaiClient, mc)
	if err != nil {
		return nil, err
	}

	return client, nil
}

func (r *ReveniumGoogle) GetConfig() *Config {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.config
}

func (r *ReveniumGoogle) GetProvider() Provider {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.provider
}

// GetGenaiClient returns the provider's own client. Calls made through it are
// neither recorded nor checked against cost controls, except polls of Veo
// operations through Operations: every finished Veo operation this client sees
// polled is recorded once, whether this client, another client or another
// process submitted it. One this client did not submit is recorded at Veo's
// default 8 seconds per video, since Google's operation does not carry the
// requested length.
func (r *ReveniumGoogle) GetGenaiClient() *genai.Client {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.client
}

func (r *ReveniumGoogle) Models() *ModelsInterface {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return &ModelsInterface{
		client:   r.client,
		config:   r.config,
		provider: r.provider,
		parent:   r,
	}
}

func (r *ReveniumGoogle) Flush() {
	core.Debug("Flushing pending metering requests...")
	r.metering.Flush()
	core.Debug("All metering requests completed")
}

func (r *ReveniumGoogle) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.enforcer.Release()
	return r.metering.Close()
}

type ModelsInterface struct {
	client   *genai.Client
	config   *Config
	provider Provider
	parent   *ReveniumGoogle
}

func (m *ModelsInterface) checkCostControls(ctx context.Context, model string) error {
	return m.parent.enforcer.Check(enforcement.NewEvalContext(ctx, model, m.provider.String()))
}

func (m *ModelsInterface) GenerateContent(
	ctx context.Context,
	model string,
	contents []*genai.Content,
	config *genai.GenerateContentConfig,
) (*genai.GenerateContentResponse, error) {
	metadata := core.GetUsageMetadata(ctx)

	core.Debug("GenerateContent called with model: %s", model)

	if err := m.checkCostControls(ctx, model); err != nil {
		core.Debug("GenerateContent blocked by enforcement: %v", err)
		return nil, err
	}

	requestTime := time.Now()
	ctx, sending := watchSending(ctx)

	resp, err := m.client.Models.GenerateContent(ctx, model, contents, config)

	completionStartTime := time.Now()
	duration := completionStartTime.Sub(requestTime)

	if err != nil {
		core.Debug("GenerateContent error: %v", err)
		if !sending.left() {
			return nil, err
		}
		payload := m.buildPayload(nil, model, false, requestTime, duration, completionStartTime, config).Build()
		metering.ApplyFailedCall(payload, callFailure(err), metadata)
		m.parent.metering.Send(payload)
		return nil, err
	}

	core.Debug("GenerateContent completed in %v, tokens: %d", duration, reportedTotalTokens(resp.UsageMetadata))

	payload := m.buildPayload(resp, model, false, requestTime, duration, completionStartTime, config).
		Build()
	metering.ApplyMetadata(payload, metadata)
	metering.ApplyChatCapture(payload, metadata, func() metering.ChatCapture {
		return contentsCapture(contents, config, responseReply(resp, "\n"))
	})
	m.parent.metering.Send(payload)

	return resp, nil
}

func (m *ModelsInterface) GenerateContentStream(
	ctx context.Context,
	model string,
	contents []*genai.Content,
	config *genai.GenerateContentConfig,
) iter.Seq2[*genai.GenerateContentResponse, error] {
	metadata := core.GetUsageMetadata(ctx)

	core.Debug("GenerateContentStream called with model: %s", model)

	if err := m.checkCostControls(ctx, model); err != nil {
		core.Debug("GenerateContentStream blocked by enforcement: %v", err)
		return func(yield func(*genai.GenerateContentResponse, error) bool) {
			yield(nil, err)
		}
	}

	requestTime := time.Now()
	ctx, sending := watchSending(ctx)

	stream := m.client.Models.GenerateContentStream(ctx, model, contents, config)

	return func(yield func(*genai.GenerateContentResponse, error) bool) {
		var outcome streamOutcome
		reply := metering.ReplyCollectorFor(metadata)
		var completionStartTime time.Time
		var firstTokenReceived bool
		chunkCount := 0

		record := func(streamErr error) {
			if streamErr != nil && !sending.left() {
				return
			}
			responseTime := time.Now()
			if !firstTokenReceived {
				completionStartTime = responseTime
			}
			payload := m.buildPayload(outcome.response(), model, true, requestTime, responseTime.Sub(requestTime), completionStartTime, config).Build()
			if outcome.abandonedBeforeFinish() {
				payload.StopReason = string(core.StopReasonCancelled)
			}
			if streamErr != nil {
				metering.ApplyFailedCall(payload, callFailure(streamErr), metadata)
			} else {
				metering.ApplyMetadata(payload, metadata)
				metering.ApplyChatCapture(payload, metadata, func() metering.ChatCapture {
					return contentsCapture(contents, config, "").WithStreamedReply(reply)
				})
			}
			m.parent.metering.Send(payload)
		}

		for resp, err := range stream {
			if err != nil {
				core.Debug("Stream error after %d chunks: %v", chunkCount, err)
				record(err)
				yield(nil, err)
				return
			}

			chunkCount++

			if !firstTokenReceived {
				completionStartTime = time.Now()
				firstTokenReceived = true
			}

			outcome.observe(resp)
			reply.Add(responseReply(resp, ""))
			if outcome.finished {
				reply.End()
			}

			if !yield(resp, nil) {
				core.Debug("Stream stopped by consumer after %d chunks", chunkCount)
				outcome.stoppedByCaller = true
				record(nil)
				return
			}
		}

		core.Debug("Stream completed: %d chunks, %d total tokens", chunkCount, reportedTotalTokens(outcome.usage))
		record(nil)
	}
}

// streamOutcome keeps usage and finishReason from the latest chunk that reports
// them, because a trailing chunk can carry usage without candidates and so without
// a finish reason. finished is sticky: it records whether any chunk's first
// candidate carried a finish reason, so a trailing usage-only chunk does not turn a
// finished stream the caller then abandoned into a cancelled one. stoppedByCaller
// comes from no chunk; the consumer loop sets it when yield returns false.
type streamOutcome struct {
	usage           *genai.GenerateContentResponseUsageMetadata
	finishReason    genai.FinishReason
	finished        bool
	stoppedByCaller bool
}

func (o *streamOutcome) observe(chunk *genai.GenerateContentResponse) {
	if chunk.UsageMetadata != nil {
		o.usage = chunk.UsageMetadata
	}
	if reason := ExtractFinishReason(chunk); reason != "" {
		o.finishReason = reason
	}
	o.finished = o.finished || firstCandidateFinished(chunk)
}

func (o *streamOutcome) abandonedBeforeFinish() bool {
	return o.stoppedByCaller && !o.finished
}

func (o *streamOutcome) response() *genai.GenerateContentResponse {
	if o.usage == nil && o.finishReason == "" {
		return nil
	}
	resp := &genai.GenerateContentResponse{UsageMetadata: o.usage}
	if o.finishReason != "" {
		resp.Candidates = []*genai.Candidate{{FinishReason: o.finishReason}}
	}
	return resp
}

func reportedTotalTokens(usage *genai.GenerateContentResponseUsageMetadata) int32 {
	if usage == nil {
		return 0
	}
	return usage.TotalTokenCount
}

// Google feeds the results of built-in tools (code execution, search grounding,
// URL context) back to the model and bills them as input, but reports them
// outside promptTokenCount.
func billedInputTokens(usage *genai.GenerateContentResponseUsageMetadata) int64 {
	return int64(usage.PromptTokenCount) + int64(usage.ToolUsePromptTokenCount)
}

// Google bills thoughts at the output rate but reports them outside candidatesTokenCount,
// and the platform prices outputTokenCount only, treating reasoningTokenCount as a
// breakdown the way OpenAI's completion count already includes reasoning.
func billedOutputTokens(usage *genai.GenerateContentResponseUsageMetadata) int64 {
	return int64(usage.CandidatesTokenCount) + int64(usage.ThoughtsTokenCount)
}

func (m *ModelsInterface) buildPayload(
	resp *genai.GenerateContentResponse,
	model string,
	isStreamed bool,
	requestTime time.Time,
	duration time.Duration,
	completionStartTime time.Time,
	config *genai.GenerateContentConfig,
) *metering.PayloadBuilder {
	var inputTokens, outputTokens, totalTokens, reasoningTokens, cacheReadTokens int64

	if resp != nil && resp.UsageMetadata != nil {
		usage := resp.UsageMetadata
		inputTokens = billedInputTokens(usage)
		reasoningTokens = int64(usage.ThoughtsTokenCount)
		outputTokens = billedOutputTokens(usage)
		totalTokens = int64(usage.TotalTokenCount)
		if totalTokens == 0 {
			totalTokens = inputTokens + outputTokens
		}
		cacheReadTokens = int64(usage.CachedContentTokenCount)
	}

	finishReason := ExtractFinishReason(resp)
	stopReason := string(MapGoogleFinishReason(finishReason, core.StopReasonEnd))

	providerStr := m.provider.String()
	modelSource := providerStr

	timeToFirstToken := completionStartTime.Sub(requestTime).Milliseconds()

	builder := metering.NewPayload(metering.OperationChat, model, providerStr).
		WithTiming(requestTime, duration).
		WithTokens(inputTokens, outputTokens, totalTokens).
		WithReasoningTokens(reasoningTokens, 0, cacheReadTokens).
		WithStreaming(isStreamed, timeToFirstToken, &completionStartTime).
		WithStopReason(stopReason).
		WithModelSource(modelSource)

	if config != nil && config.Temperature != nil {
		builder = builder.WithTemperature(float64(*config.Temperature))
	}

	if score := ExtractConfidenceScore(resp); score != nil {
		builder = builder.WithResponseQualityScore(score)
	}

	return builder
}

func Reset() {
	_ = sharedClient.ResetAndClose()
}
