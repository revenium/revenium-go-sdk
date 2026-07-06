package grok

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/revenium/revenium-go-sdk/core/metering"
)

type ReveniumGrok struct {
	config   *Config
	provider Provider
	mu       sync.RWMutex
	client   *http.Client
	metering *metering.MeteringClient
}

var (
	globalClient *ReveniumGrok
	globalMu     sync.RWMutex
	initialized  bool
)

func Initialize(opts ...Option) error {
	globalMu.Lock()
	defer globalMu.Unlock()

	if initialized {
		return nil
	}

	core.InitializeLogger()
	core.Info("Initializing Revenium middleware for xAI Grok...")

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

	provider := DetectProvider(cfg)

	mc, err := metering.NewMeteringClient(metering.MeteringClientConfig{
		APIKey:  cfg.Revenium.APIKey,
		BaseURL: cfg.Revenium.BaseURL,
	})
	if err != nil {
		return err
	}

	globalClient = &ReveniumGrok{
		config:   cfg,
		provider: provider,
		client: &http.Client{
			Timeout: 60 * time.Second,
		},
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

func GetClient() (*ReveniumGrok, error) {
	globalMu.RLock()
	defer globalMu.RUnlock()

	if !initialized {
		return nil, core.NewConfigError("middleware not initialized, call Initialize() first", nil)
	}

	return globalClient, nil
}

func NewReveniumGrok(cfg *Config) (*ReveniumGrok, error) {
	if cfg == nil {
		return nil, core.NewConfigError("config cannot be nil", nil)
	}

	if cfg.Revenium == nil || cfg.Revenium.APIKey == "" {
		return nil, core.NewConfigError("REVENIUM_METERING_API_KEY is required", nil)
	}

	provider := DetectProvider(cfg)

	mc, err := metering.NewMeteringClient(metering.MeteringClientConfig{
		APIKey:  cfg.Revenium.APIKey,
		BaseURL: cfg.Revenium.BaseURL,
	})
	if err != nil {
		return nil, err
	}

	return &ReveniumGrok{
		config:   cfg,
		provider: provider,
		client: &http.Client{
			Timeout: 60 * time.Second,
		},
		metering: mc,
	}, nil
}

func (r *ReveniumGrok) GetConfig() *Config {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.config
}

func (r *ReveniumGrok) GetProvider() Provider {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.provider
}

func (r *ReveniumGrok) ChatCompletions(ctx context.Context, req ChatCompletionRequest) (*ChatCompletionResponse, error) {
	metadata := core.GetUsageMetadata(ctx)
	startTime := time.Now()

	resp, err := r.callGrokAPI(ctx, req)
	if err != nil {
		return nil, err
	}

	duration := time.Since(startTime)

	opType := metering.OperationChat
	if hasVisionContent(req.Messages) {
		opType = metering.OperationVision
	}

	stopReason := "END"
	if len(resp.Choices) > 0 && resp.Choices[0].FinishReason != "" {
		stopReason = mapStopReasonToRevenium(resp.Choices[0].FinishReason)
	}

	var reasoningTokens, cacheReadTokens int
	if resp.Usage.CompletionTokensDetails != nil {
		reasoningTokens = resp.Usage.CompletionTokensDetails.ReasoningTokens
	}
	if resp.Usage.PromptTokensDetails != nil {
		cacheReadTokens = resp.Usage.PromptTokensDetails.CachedTokens
	}

	payload := metering.NewPayload(opType, resp.Model, "xai").
		WithTiming(startTime, duration).
		WithTokens(int64(resp.Usage.PromptTokens), int64(resp.Usage.CompletionTokens), int64(resp.Usage.TotalTokens)).
		WithReasoningTokens(int64(reasoningTokens), 0, int64(cacheReadTokens)).
		WithStopReason(stopReason).
		WithModelSource("XAI").
		WithTransactionID(resp.ID).
		WithSystemFingerprint(resp.SystemFingerprint).
		Build()

	if req.Temperature != nil {
		payload.Temperature = req.Temperature
	}

	metering.ApplyMetadata(payload, metadata)
	r.metering.Send(payload)

	return resp, nil
}

func (r *ReveniumGrok) callGrokAPI(ctx context.Context, req ChatCompletionRequest) (*ChatCompletionResponse, error) {
	baseURL := r.config.BaseURL
	if baseURL == "" {
		baseURL = "https://api.x.ai/v1"
	}
	url := baseURL + "/chat/completions"

	jsonData, err := json.Marshal(req)
	if err != nil {
		return nil, core.NewProviderError("failed to marshal request", err)
	}

	core.Debug("Calling xAI Grok API: %s", url)

	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, core.NewProviderError("failed to create request", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	if r.config.XAIAPIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+r.config.XAIAPIKey)
	}

	httpResp, err := r.client.Do(httpReq)
	if err != nil {
		return nil, core.NewNetworkError("xAI Grok API request failed", err)
	}
	defer httpResp.Body.Close()

	body, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, core.NewNetworkError("failed to read response body", err)
	}

	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		return nil, core.NewProviderError(
			fmt.Sprintf("xAI Grok API returned %d: %s", httpResp.StatusCode, string(body)),
			nil,
		)
	}

	var resp ChatCompletionResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, core.NewProviderError("failed to parse response", err)
	}

	return &resp, nil
}

func (r *ReveniumGrok) Flush() {
	r.metering.Flush()
}

func (r *ReveniumGrok) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.metering.Close()
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
