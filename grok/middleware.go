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
	"github.com/revenium/revenium-go-sdk/core/enforcement"
	"github.com/revenium/revenium-go-sdk/core/metering"
	"github.com/revenium/revenium-go-sdk/core/prompt"
)

type ReveniumGrok struct {
	config   *Config
	provider Provider
	mu       sync.RWMutex
	client   *http.Client
	metering *metering.MeteringClient
	enforcer *enforcement.Engine
}

var sharedClient = core.NewSharedClient("NewReveniumGrok", func(c *ReveniumGrok) *core.ReveniumConfig { return c.config.Revenium })

func Initialize(opts ...Option) error {
	cfg := &Config{}
	for _, opt := range opts {
		opt(cfg)
	}
	return sharedClient.Initialize(core.NamedAccount(cfg.Revenium), func() (*ReveniumGrok, error) {
		return buildSharedClient(cfg)
	})
}

func buildSharedClient(cfg *Config) (*ReveniumGrok, error) {
	core.InitializeLogger()
	core.Info("Initializing Revenium middleware for xAI Grok...")

	if err := cfg.loadFromEnv(); err != nil {
		core.Warn("Failed to load configuration from environment: %v", err)
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	provider := DetectProvider(cfg)

	mc, err := metering.NewMeteringClient(metering.ClientConfigFor(cfg.Revenium))
	if err != nil {
		return nil, err
	}

	candidate := &ReveniumGrok{
		config:   cfg,
		provider: provider,
		client: &http.Client{
			Timeout: 60 * time.Second,
		},
		metering: mc,
		enforcer: enforcement.StartFor(cfg.Revenium),
	}

	core.Info("Revenium middleware initialized successfully")
	return candidate, nil
}

func IsInitialized() bool {
	return sharedClient.IsInitialized()
}

func GetClient() (*ReveniumGrok, error) {
	return sharedClient.Get()
}

func NewReveniumGrok(cfg *Config) (*ReveniumGrok, error) {
	if cfg == nil {
		return nil, core.NewConfigError("config cannot be nil", nil)
	}
	core.TrimReveniumConfig(cfg.Revenium)

	if cfg.Revenium == nil || cfg.Revenium.APIKey == "" {
		return nil, core.NewConfigError("REVENIUM_METERING_API_KEY is required", nil)
	}

	provider := DetectProvider(cfg)

	mc, err := metering.NewMeteringClient(metering.ClientConfigFor(cfg.Revenium))
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
		enforcer: enforcement.StartFor(cfg.Revenium),
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
	if req.Stream {
		return nil, ErrStreamingNotSupported
	}

	metadata := core.GetUsageMetadata(ctx)

	if err := r.enforcer.Check(enforcement.NewEvalContext(ctx, req.Model, ProviderXAI.String()).ServedModelMayBeDated()); err != nil {
		core.Debug("ChatCompletions blocked by enforcement: %v", err)
		return nil, err
	}

	opType := metering.OperationChat
	if hasVisionContent(req.Messages) {
		opType = metering.OperationVision
	}

	startTime := time.Now()

	resp, err := r.callGrokAPI(ctx, req)
	if err != nil {
		r.recordFailure(opType, req.Model, startTime, err, metadata)
		return nil, err
	}

	duration := time.Since(startTime)

	stopReason := "END"
	if len(resp.Choices) > 0 && resp.Choices[0].FinishReason != "" {
		stopReason = mapStopReasonToRevenium(resp.Choices[0].FinishReason)
	}

	usage := resp.Usage
	payload := metering.NewPayload(opType, meteredModel(resp.Model, req.Model), ProviderXAI.String()).
		WithTiming(startTime, duration).
		WithTokens(int64(usage.PromptTokens), usage.billedOutputTokens(), int64(usage.TotalTokens)).
		WithReasoningTokens(usage.reasoningTokens(), 0, usage.cacheReadTokens()).
		WithCosts(nil, nil, nil, nil, usage.reportedCostUSD()).
		WithStopReason(stopReason).
		WithModelSource("XAI").
		WithTransactionID(resp.ID).
		WithSystemFingerprint(resp.SystemFingerprint).
		Build()

	if req.Temperature != nil {
		payload.Temperature = req.Temperature
	}

	metering.ApplyMetadata(payload, metadata)
	metering.ApplyChatCapture(payload, metadata, func() metering.ChatCapture {
		return metering.OpenAIChatCapture(req.Messages, completionReply(resp))
	})
	r.metering.Send(payload)

	return resp, nil
}

func (r *ReveniumGrok) recordFailure(opType metering.OperationType, model string, startTime time.Time, err error, metadata map[string]interface{}) {
	payload := metering.NewPayload(opType, model, ProviderXAI.String()).
		WithTiming(startTime, time.Since(startTime)).
		WithModelSource("XAI").
		Build()
	metering.ApplyFailedCall(payload, metering.FailureOf(err), metadata)
	r.metering.Send(payload)
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
		).WithProviderAnswer(httpResp.StatusCode, body)
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
	r.enforcer.Release()
	return r.metering.Close()
}

func Reset() {
	_ = sharedClient.ResetAndClose()
}

func completionReply(resp *ChatCompletionResponse) string {
	if len(resp.Choices) == 0 {
		return ""
	}
	return prompt.ContentText(resp.Choices[0].Message.Content)
}
