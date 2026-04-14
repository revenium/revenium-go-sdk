package litellm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/revenium/revenium-go-sdk/core/metering"
)

type ReveniumLiteLLM struct {
	config     *Config
	httpClient *http.Client
	metering   *metering.MeteringClient
	mu         sync.RWMutex
	enabled    atomic.Bool
}

var (
	globalClient *ReveniumLiteLLM
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

	core.Info("Initializing Revenium LiteLLM middleware...")

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

	globalClient = &ReveniumLiteLLM{
		config:     cfg,
		httpClient: &http.Client{Timeout: 120 * time.Second},
		metering:   mc,
	}
	globalClient.enabled.Store(true)

	initialized = true
	core.Info("Revenium LiteLLM middleware initialized successfully (proxy: %s)", cfg.LiteLLMProxyURL)
	return nil
}

func IsInitialized() bool {
	globalMu.RLock()
	defer globalMu.RUnlock()
	return initialized
}

func GetClient() (*ReveniumLiteLLM, error) {
	globalMu.RLock()
	defer globalMu.RUnlock()

	if !initialized {
		return nil, core.NewConfigError("middleware not initialized, call Initialize() first", nil)
	}

	return globalClient, nil
}

func NewReveniumLiteLLM(cfg *Config) (*ReveniumLiteLLM, error) {
	if cfg == nil {
		return nil, core.NewConfigError("config cannot be nil", nil)
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

	client := &ReveniumLiteLLM{
		config:     cfg,
		httpClient: &http.Client{Timeout: 120 * time.Second},
		metering:   mc,
	}
	client.enabled.Store(true)
	return client, nil
}

func (r *ReveniumLiteLLM) GetConfig() *Config {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.config
}

func (r *ReveniumLiteLLM) Chat() *ChatInterface {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return &ChatInterface{parent: r}
}

func (r *ReveniumLiteLLM) Embeddings() *EmbeddingsInterface {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return &EmbeddingsInterface{parent: r}
}

func (r *ReveniumLiteLLM) Flush() {
	core.Debug("Flushing pending metering requests...")
	r.metering.Flush()
	core.Debug("All metering requests completed")
}

func (r *ReveniumLiteLLM) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.metering.Close()
}

type ChatInterface struct {
	parent *ReveniumLiteLLM
}

func (c *ChatInterface) Completions() *CompletionsInterface {
	return &CompletionsInterface{parent: c.parent}
}

type CompletionsInterface struct {
	parent *ReveniumLiteLLM
}

func (c *CompletionsInterface) New(ctx context.Context, req ChatCompletionRequest) (*ChatCompletionResponse, error) {
	metadata := core.GetUsageMetadata(ctx)
	req.Stream = false

	requestTime := time.Now()

	resp, respHeaders, err := c.doRequest(ctx, "/chat/completions", req)
	metadata = MergeMetadata(metadata, ExtractMetadataFromHeaders(respHeaders))
	if err != nil {
		duration := time.Since(requestTime)
		modelName := ExtractModelName(req.Model)
		provider := ExtractProvider(req.Model)
		modelSource := ExtractModelSource(req.Model)

		payload := metering.NewPayload(metering.OperationChat, modelName, provider).
			WithTiming(requestTime, duration).
			WithTokens(0, 0, 0).
			WithReasoningTokens(0, 0, 0).
			WithStreaming(false, 0, nil).
			WithModelSource(modelSource).
			WithError(err.Error()).
			Build()
		metering.ApplyMetadata(payload, metadata)
		if c.parent.IsEnabled() {
			c.parent.metering.Send(payload)
		}
		return nil, err
	}

	duration := time.Since(requestTime)

	var inputTokens, outputTokens, totalTokens, reasoningTokens, cacheReadTokens, cacheCreationTokens int64
	if resp.Usage != nil {
		inputTokens = resp.Usage.PromptTokens
		outputTokens = resp.Usage.CompletionTokens
		totalTokens = resp.Usage.TotalTokens
		if resp.Usage.CompletionTokensDetails != nil {
			reasoningTokens = resp.Usage.CompletionTokensDetails.ReasoningTokens
		}
		if resp.Usage.PromptTokensDetails != nil {
			cacheReadTokens = resp.Usage.PromptTokensDetails.CachedTokens
		}
	}

	finishReason := ""
	if len(resp.Choices) > 0 {
		finishReason = resp.Choices[0].FinishReason
	}
	stopReason := string(MapFinishReason(finishReason, core.StopReasonEnd))

	modelName := ExtractModelName(resp.Model)
	provider := ExtractProvider(resp.Model)
	modelSource := ExtractModelSource(resp.Model)

	payload := metering.NewPayload(metering.OperationChat, modelName, provider).
		WithTiming(requestTime, duration).
		WithTokens(inputTokens, outputTokens, totalTokens).
		WithReasoningTokens(reasoningTokens, cacheCreationTokens, cacheReadTokens).
		WithStreaming(false, 0, nil).
		WithStopReason(stopReason).
		WithModelSource(modelSource).
		WithSystemFingerprint(resp.SystemFingerprint).
		Build()
	metering.ApplyMetadata(payload, metadata)
	if c.parent.IsEnabled() {
		c.parent.metering.Send(payload)
	}

	return resp, nil
}

func (c *CompletionsInterface) NewStreaming(ctx context.Context, req ChatCompletionRequest) (*StreamingResponse, error) {
	metadata := core.GetUsageMetadata(ctx)
	req.Stream = true
	if req.StreamOptions == nil {
		req.StreamOptions = &StreamOptions{IncludeUsage: true}
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, core.NewInternalError("failed to marshal request", err)
	}

	proxyURL := strings.TrimRight(c.parent.config.LiteLLMProxyURL, "/")
	url := proxyURL + "/chat/completions"

	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, core.NewNetworkError("failed to create request", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	if c.parent.config.LiteLLMAPIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.parent.config.LiteLLMAPIKey)
	}

	httpResp, err := c.parent.httpClient.Do(httpReq)
	if err != nil {
		return nil, core.NewNetworkError("request to LiteLLM proxy failed", err)
	}

	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(httpResp.Body)
		httpResp.Body.Close()
		return nil, core.NewProviderError(
			fmt.Sprintf("LiteLLM proxy returned %d: %s", httpResp.StatusCode, string(respBody)),
			nil,
		)
	}

	mergedMetadata := MergeMetadata(metadata, ExtractMetadataFromHeaders(httpResp.Header))
	stream := newStreamingResponse(httpResp.Body, mergedMetadata, req.Model, c.parent.metering, c.parent.IsEnabled())
	return stream, nil
}

func (c *CompletionsInterface) doRequest(ctx context.Context, path string, req ChatCompletionRequest) (*ChatCompletionResponse, http.Header, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, nil, core.NewInternalError("failed to marshal request", err)
	}

	proxyURL := strings.TrimRight(c.parent.config.LiteLLMProxyURL, "/")
	url := proxyURL + path

	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, nil, core.NewNetworkError("failed to create request", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	if c.parent.config.LiteLLMAPIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.parent.config.LiteLLMAPIKey)
	}

	core.Debug("Sending request to %s", url)

	httpResp, err := c.parent.httpClient.Do(httpReq)
	if err != nil {
		return nil, nil, core.NewNetworkError("request to LiteLLM proxy failed", err)
	}
	defer httpResp.Body.Close()

	respBody, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, nil, core.NewNetworkError("failed to read response body", err)
	}

	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		return nil, httpResp.Header, core.NewProviderError(
			fmt.Sprintf("LiteLLM proxy returned %d: %s", httpResp.StatusCode, string(respBody)),
			nil,
		)
	}

	var resp ChatCompletionResponse
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, httpResp.Header, core.NewInternalError("failed to parse response", err)
	}

	return &resp, httpResp.Header, nil
}

type EmbeddingsInterface struct {
	parent *ReveniumLiteLLM
}

func (e *EmbeddingsInterface) New(ctx context.Context, req EmbeddingRequest) (*EmbeddingResponse, error) {
	metadata := core.GetUsageMetadata(ctx)
	requestTime := time.Now()

	body, err := json.Marshal(req)
	if err != nil {
		return nil, core.NewInternalError("failed to marshal request", err)
	}

	proxyURL := strings.TrimRight(e.parent.config.LiteLLMProxyURL, "/")
	url := proxyURL + "/embeddings"

	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, core.NewNetworkError("failed to create request", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	if e.parent.config.LiteLLMAPIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+e.parent.config.LiteLLMAPIKey)
	}

	core.Debug("Sending embeddings request to %s", url)

	httpResp, err := e.parent.httpClient.Do(httpReq)
	if err != nil {
		duration := time.Since(requestTime)
		modelName := ExtractModelName(req.Model)
		provider := ExtractProvider(req.Model)
		modelSource := ExtractModelSource(req.Model)

		payload := metering.NewPayload(metering.OperationEmbed, modelName, provider).
			WithTiming(requestTime, duration).
			WithTokens(0, 0, 0).
			WithReasoningTokens(0, 0, 0).
			WithStreaming(false, 0, nil).
			WithModelSource(modelSource).
			WithError(err.Error()).
			Build()
		metering.ApplyMetadata(payload, metadata)
		if e.parent.IsEnabled() {
			e.parent.metering.Send(payload)
		}
		return nil, core.NewNetworkError("request to LiteLLM proxy failed", err)
	}
	defer httpResp.Body.Close()

	metadata = MergeMetadata(metadata, ExtractMetadataFromHeaders(httpResp.Header))

	respBody, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, core.NewNetworkError("failed to read response body", err)
	}

	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		return nil, core.NewProviderError(
			fmt.Sprintf("LiteLLM proxy returned %d: %s", httpResp.StatusCode, string(respBody)),
			nil,
		)
	}

	var resp EmbeddingResponse
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, core.NewInternalError("failed to parse response", err)
	}

	duration := time.Since(requestTime)
	modelName := ExtractModelName(req.Model)
	provider := ExtractProvider(req.Model)
	modelSource := ExtractModelSource(req.Model)

	payload := metering.NewPayload(metering.OperationEmbed, modelName, provider).
		WithTiming(requestTime, duration).
		WithTokens(resp.Usage.PromptTokens, 0, resp.Usage.TotalTokens).
		WithReasoningTokens(0, 0, 0).
		WithStreaming(false, 0, nil).
		WithStopReason("END").
		WithModelSource(modelSource).
		Build()
	metering.ApplyMetadata(payload, metadata)
	if e.parent.IsEnabled() {
		e.parent.metering.Send(payload)
	}

	return &resp, nil
}

func ResetGlobalState() {
	globalMu.Lock()
	defer globalMu.Unlock()
	if globalClient != nil {
		globalClient.Close()
	}
	globalClient = nil
	initialized = false
}
