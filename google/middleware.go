package google

import (
	"context"
	"iter"
	"sync"
	"time"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/revenium/revenium-go-sdk/core/metering"
	"google.golang.org/genai"
)

type ReveniumGoogle struct {
	client   *genai.Client
	config   *Config
	provider Provider
	mu       sync.RWMutex
	metering *metering.MeteringClient
}

var (
	globalClient *ReveniumGoogle
	globalMu     sync.RWMutex
	initialized  bool
)

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
	return metering.NewMeteringClient(metering.MeteringClientConfig{
		APIKey:  cfg.Revenium.APIKey,
		BaseURL: cfg.Revenium.BaseURL,
	})
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

	if cfg.Revenium != nil {
		core.SetGlobalDebug(cfg.Revenium.Debug)
	}

	if err := cfg.Validate(); err != nil {
		return err
	}

	provider := DetectProvider(cfg)

	ctx := context.Background()
	genaiClient, err := createGenaiClient(ctx, cfg, provider)
	if err != nil {
		return core.NewProviderError("failed to create Google Genai client", err)
	}

	mc, err := newMeteringClient(cfg)
	if err != nil {
		return err
	}

	globalClient = &ReveniumGoogle{
		client:   genaiClient,
		config:   cfg,
		provider: provider,
		metering: mc,
	}

	initialized = true
	core.Info("Revenium middleware initialized successfully with provider: %s", provider.String())
	return nil
}

func IsInitialized() bool {
	globalMu.RLock()
	defer globalMu.RUnlock()
	return initialized
}

func GetClient() (*ReveniumGoogle, error) {
	globalMu.RLock()
	defer globalMu.RUnlock()

	if !initialized {
		return nil, core.NewConfigError("middleware not initialized, call Initialize() first", nil)
	}

	return globalClient, nil
}

func NewReveniumGoogle(cfg *Config) (*ReveniumGoogle, error) {
	if cfg == nil {
		return nil, core.NewConfigError("config cannot be nil", nil)
	}

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

	return &ReveniumGoogle{
		client:   genaiClient,
		config:   cfg,
		provider: provider,
		metering: mc,
	}, nil
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
	return r.metering.Close()
}

type ModelsInterface struct {
	client   *genai.Client
	config   *Config
	provider Provider
	parent   *ReveniumGoogle
}

func (m *ModelsInterface) GenerateContent(
	ctx context.Context,
	model string,
	contents []*genai.Content,
	config *genai.GenerateContentConfig,
) (*genai.GenerateContentResponse, error) {
	metadata := core.GetUsageMetadata(ctx)

	core.Debug("GenerateContent called with model: %s", model)

	requestTime := time.Now()

	resp, err := m.client.Models.GenerateContent(ctx, model, contents, config)

	completionStartTime := time.Now()
	duration := completionStartTime.Sub(requestTime)

	if err != nil {
		core.Debug("GenerateContent error: %v", err)
		payload := m.buildPayload(nil, model, false, requestTime, duration, completionStartTime, config).
			WithError(err.Error()).
			Build()
		metering.ApplyMetadata(payload, metadata)
		m.parent.metering.Send(payload)
		return nil, err
	}

	core.Debug("GenerateContent completed in %v, tokens: %d", duration, resp.UsageMetadata.TotalTokenCount)

	payload := m.buildPayload(resp, model, false, requestTime, duration, completionStartTime, config).
		Build()
	metering.ApplyMetadata(payload, metadata)
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

	requestTime := time.Now()

	stream := m.client.Models.GenerateContentStream(ctx, model, contents, config)

	return func(yield func(*genai.GenerateContentResponse, error) bool) {
		var lastUsage *genai.GenerateContentResponseUsageMetadata
		var completionStartTime time.Time
		var firstTokenReceived bool
		chunkCount := 0

		for resp, err := range stream {
			if err != nil {
				core.Debug("Stream error after %d chunks: %v", chunkCount, err)
				responseTime := time.Now()
				if !firstTokenReceived {
					completionStartTime = responseTime
				}
				duration := responseTime.Sub(requestTime)
				var streamResp *genai.GenerateContentResponse
				if lastUsage != nil {
					streamResp = &genai.GenerateContentResponse{UsageMetadata: lastUsage}
				}
				payload := m.buildPayload(streamResp, model, true, requestTime, duration, completionStartTime, config).
					WithError(err.Error()).
					Build()
				metering.ApplyMetadata(payload, metadata)
				m.parent.metering.Send(payload)
				if !yield(nil, err) {
					return
				}
				return
			}

			chunkCount++

			if !firstTokenReceived {
				completionStartTime = time.Now()
				firstTokenReceived = true
			}

			if resp.UsageMetadata != nil {
				lastUsage = resp.UsageMetadata
			}

			if !yield(resp, nil) {
				core.Debug("Stream stopped by consumer after %d chunks", chunkCount)
				responseTime := time.Now()
				if lastUsage != nil {
					duration := responseTime.Sub(requestTime)
					payload := m.buildPayload(&genai.GenerateContentResponse{UsageMetadata: lastUsage}, model, true, requestTime, duration, completionStartTime, config).
						Build()
					metering.ApplyMetadata(payload, metadata)
					m.parent.metering.Send(payload)
				}
				return
			}
		}

		responseTime := time.Now()
		if lastUsage != nil {
			duration := responseTime.Sub(requestTime)
			core.Debug("Stream completed: %d chunks, %d total tokens in %v", chunkCount, lastUsage.TotalTokenCount, duration)
			payload := m.buildPayload(&genai.GenerateContentResponse{UsageMetadata: lastUsage}, model, true, requestTime, duration, completionStartTime, config).
				Build()
			metering.ApplyMetadata(payload, metadata)
			m.parent.metering.Send(payload)
		}
	}
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
		inputTokens = int64(usage.PromptTokenCount)
		outputTokens = int64(usage.CandidatesTokenCount)
		totalTokens = int64(usage.TotalTokenCount)
		if totalTokens == 0 {
			totalTokens = inputTokens + outputTokens
		}
		cacheReadTokens = int64(usage.CachedContentTokenCount)
		reasoningTokens = int64(usage.ThoughtsTokenCount)
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
	globalMu.Lock()
	defer globalMu.Unlock()

	if globalClient != nil {
		globalClient.Close()
		globalClient = nil
	}

	initialized = false
}
