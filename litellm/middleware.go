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
	"github.com/revenium/revenium-go-sdk/core/enforcement"
	"github.com/revenium/revenium-go-sdk/core/metering"
)

type ReveniumLiteLLM struct {
	config      *Config
	httpClient  *http.Client
	metering    *metering.MeteringClient
	enforcer    *enforcement.Engine
	modelRoutes modelRoutes
	proxyPeople proxyPeople
	mu          sync.RWMutex
	enabled     atomic.Bool
}

var sharedClient = core.NewSharedClient("NewReveniumLiteLLM", func(c *ReveniumLiteLLM) *core.ReveniumConfig { return c.config.Revenium })

func Initialize(opts ...Option) error {
	cfg := &Config{}
	for _, opt := range opts {
		opt(cfg)
	}
	return sharedClient.Initialize(core.NamedAccount(cfg.Revenium), func() (*ReveniumLiteLLM, error) {
		return buildSharedClient(cfg)
	})
}

func buildSharedClient(cfg *Config) (*ReveniumLiteLLM, error) {
	if err := cfg.loadFromEnv(); err != nil {
		core.Warn("Failed to load configuration from environment: %v", err)
	}

	core.Info("Initializing Revenium LiteLLM middleware...")

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	mc, err := metering.NewMeteringClient(metering.ClientConfigFor(cfg.Revenium))
	if err != nil {
		return nil, err
	}

	candidate := &ReveniumLiteLLM{
		config:     cfg,
		httpClient: &http.Client{Timeout: 120 * time.Second},
		metering:   mc,
		enforcer:   enforcement.StartFor(cfg.Revenium),
	}
	candidate.enabled.Store(true)

	core.Info("Revenium LiteLLM middleware initialized successfully (proxy: %s)", cfg.LiteLLMProxyURL)
	return candidate, nil
}

func IsInitialized() bool {
	return sharedClient.IsInitialized()
}

func GetClient() (*ReveniumLiteLLM, error) {
	return sharedClient.Get()
}

func NewReveniumLiteLLM(cfg *Config) (*ReveniumLiteLLM, error) {
	if cfg == nil {
		return nil, core.NewConfigError("config cannot be nil", nil)
	}
	core.TrimReveniumConfig(cfg.Revenium)

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	mc, err := metering.NewMeteringClient(metering.ClientConfigFor(cfg.Revenium))
	if err != nil {
		return nil, err
	}

	client := &ReveniumLiteLLM{
		config:     cfg,
		httpClient: &http.Client{Timeout: 120 * time.Second},
		metering:   mc,
		enforcer:   enforcement.StartFor(cfg.Revenium),
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
	r.enforcer.Release()
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

	if err := c.parent.checkCostControls(ctx, req.Model, req); err != nil {
		core.Debug("Chat.Completions.New blocked by enforcement: %v", err)
		return nil, err
	}

	requestTime := time.Now()

	resp, respHeaders, err := c.doRequest(ctx, "/chat/completions", req)
	metadata = meteredMetadata(metadata, respHeaders)
	if err != nil {
		if c.parent.IsEnabled() {
			c.parent.metering.Send(failedCallRecord(metering.OperationChat, req.Model, false, requestTime, err, metadata))
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
		cacheCreationTokens = resp.Usage.CacheCreationTokenCount()
	}

	finishReason := ""
	if len(resp.Choices) > 0 {
		finishReason = resp.Choices[0].FinishReason
	}
	stopReason := string(MapFinishReason(finishReason, core.StopReasonEnd))

	servedModel := recordedModel(resp.Model, req.Model, c.parent.servedDeployment(respHeaders), respHeaders)
	modelName := ExtractModelName(servedModel)
	provider := ExtractProvider(servedModel)
	modelSource := ExtractModelSource(servedModel)

	payload := metering.NewPayload(metering.OperationChat, modelName, provider).
		WithTiming(requestTime, duration).
		WithTokens(billableInputTokens(servedModel, inputTokens, cacheReadTokens, cacheCreationTokens), outputTokens, totalTokens).
		WithReasoningTokens(reasoningTokens, cacheCreationTokens, cacheReadTokens).
		WithCacheCreationByTTL(resp.Usage.CacheCreationByTTL()).
		WithCosts(nil, nil, nil, nil, reportedCost(resp.Usage.ReportedCost(), respHeaders)).
		WithStreaming(false, 0, nil).
		WithStopReason(stopReason).
		WithModelSource(modelSource).
		WithSystemFingerprint(resp.SystemFingerprint).
		Build()
	metering.ApplyMetadata(payload, metadata)
	metering.ApplyChatCapture(payload, metadata, func() metering.ChatCapture {
		return metering.OpenAIChatCapture(req.Messages, completionReply(resp))
	})
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

	if err := c.parent.checkCostControls(ctx, req.Model, req); err != nil {
		core.Debug("Chat.Completions.NewStreaming blocked by enforcement: %v", err)
		return nil, err
	}

	requestTime := time.Now()
	httpResp, err := c.parent.send(ctx, "/chat/completions", req)
	if err != nil {
		if httpResp != nil {
			metadata = meteredMetadata(metadata, httpResp.Header)
		}
		if c.parent.IsEnabled() {
			c.parent.metering.Send(failedCallRecord(metering.OperationChat, req.Model, true, requestTime, err, metadata))
		}
		return nil, err
	}

	stream := newStreamingResponse(httpResp.Body, meteredMetadata(metadata, httpResp.Header), req.Model, c.parent.metering, c.parent.IsEnabled())
	stream.messages = req.Messages
	stream.deployment = c.parent.servedDeployment(httpResp.Header)
	stream.headers = httpResp.Header
	return stream, nil
}

func (c *CompletionsInterface) doRequest(ctx context.Context, path string, req ChatCompletionRequest) (*ChatCompletionResponse, http.Header, error) {
	var resp ChatCompletionResponse
	headers, err := c.parent.postJSON(ctx, path, req, &resp)
	if err != nil {
		return nil, headers, err
	}
	return &resp, headers, nil
}

// send posts payload to the proxy. On a 2xx answer it returns the response,
// whose body the caller closes. On any other answer it returns the response,
// its body already read and closed, together with the provider error, so the
// caller can still read the attribution headers. A request that got no answer
// returns a nil response.
func (r *ReveniumLiteLLM) send(ctx context.Context, path string, payload interface{}) (*http.Response, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, core.NewInternalError("failed to marshal request", err)
	}

	url := strings.TrimRight(r.config.LiteLLMProxyURL, "/") + path
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, core.NewNetworkError("failed to create request", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	if r.config.LiteLLMAPIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+r.config.LiteLLMAPIKey)
	}

	core.Debug("Sending request to %s", url)

	httpResp, err := r.httpClient.Do(httpReq)
	if err != nil {
		return nil, core.NewNetworkError("request to LiteLLM proxy failed", err)
	}
	r.rememberProxyPerson(payload, httpResp)

	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(httpResp.Body)
		httpResp.Body.Close()
		return httpResp, core.NewProviderError(
			fmt.Sprintf("LiteLLM proxy returned %d: %s", httpResp.StatusCode, string(respBody)),
			nil,
		).WithProviderAnswer(httpResp.StatusCode, respBody)
	}
	return httpResp, nil
}

// postJSON sends payload and decodes the proxy's answer into out. The
// answer's headers come back on failure too, for the attribution they carry.
func (r *ReveniumLiteLLM) postJSON(ctx context.Context, path string, payload, out interface{}) (http.Header, error) {
	httpResp, err := r.send(ctx, path, payload)
	if httpResp == nil {
		return nil, err
	}
	if err != nil {
		return httpResp.Header, err
	}
	defer httpResp.Body.Close()

	respBody, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return httpResp.Header, core.NewNetworkError("failed to read response body", err)
	}
	if err := json.Unmarshal(respBody, out); err != nil {
		return httpResp.Header, core.NewInternalError("failed to parse response", err)
	}
	return httpResp.Header, nil
}

type EmbeddingsInterface struct {
	parent *ReveniumLiteLLM
}

func (e *EmbeddingsInterface) New(ctx context.Context, req EmbeddingRequest) (*EmbeddingResponse, error) {
	metadata := core.GetUsageMetadata(ctx)
	if err := e.parent.checkEmbeddingCostControls(ctx, req.Model, req); err != nil {
		core.Debug("Embeddings.New blocked by enforcement: %v", err)
		return nil, err
	}
	requestTime := time.Now()

	var resp EmbeddingResponse
	headers, err := e.parent.postJSON(ctx, "/embeddings", req, &resp)
	metadata = meteredMetadata(metadata, headers)
	if err != nil {
		if e.parent.IsEnabled() {
			e.parent.metering.Send(failedCallRecord(metering.OperationEmbed, req.Model, false, requestTime, err, metadata))
		}
		return nil, err
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
	_ = sharedClient.ResetAndClose()
}

func completionReply(resp *ChatCompletionResponse) string {
	if len(resp.Choices) == 0 {
		return ""
	}
	return resp.Choices[0].Message.Content
}
