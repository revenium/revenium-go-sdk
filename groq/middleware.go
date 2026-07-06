package groq

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

type ChatCompletionRequest struct {
	Model            string                 `json:"model"`
	Messages         []ChatMessage          `json:"messages"`
	Temperature      *float64               `json:"temperature,omitempty"`
	TopP             *float64               `json:"top_p,omitempty"`
	N                *int                   `json:"n,omitempty"`
	Stream           bool                   `json:"stream,omitempty"`
	Stop             interface{}            `json:"stop,omitempty"`
	MaxTokens        *int                   `json:"max_tokens,omitempty"`
	PresencePenalty  *float64               `json:"presence_penalty,omitempty"`
	FrequencyPenalty *float64               `json:"frequency_penalty,omitempty"`
	LogitBias        map[string]int         `json:"logit_bias,omitempty"`
	User             string                 `json:"user,omitempty"`
	ResponseFormat   map[string]interface{} `json:"response_format,omitempty"`
	Seed             *int                   `json:"seed,omitempty"`
	Tools            []Tool                 `json:"tools,omitempty"`
	ToolChoice       interface{}            `json:"tool_choice,omitempty"`
}

type ChatMessage struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	Name       string     `json:"name,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

type Tool struct {
	Type     string       `json:"type"`
	Function FunctionTool `json:"function"`
}

type FunctionTool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	Parameters  map[string]interface{} `json:"parameters,omitempty"`
}

type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function FunctionCall `json:"function"`
}

type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type ChatCompletionResponse struct {
	ID                string        `json:"id"`
	Object            string        `json:"object"`
	Created           int64         `json:"created"`
	Model             string        `json:"model"`
	Choices           []Choice      `json:"choices"`
	Usage             Usage         `json:"usage"`
	SystemFingerprint string        `json:"system_fingerprint,omitempty"`
	XGroq             *GroqMetadata `json:"x_groq,omitempty"`
}

type Choice struct {
	Index        int         `json:"index"`
	Message      ChatMessage `json:"message"`
	FinishReason string      `json:"finish_reason"`
	Logprobs     interface{} `json:"logprobs,omitempty"`
}

type Usage struct {
	PromptTokens            int                              `json:"prompt_tokens"`
	CompletionTokens        int                              `json:"completion_tokens"`
	TotalTokens             int                              `json:"total_tokens"`
	PromptTokensDetails     *metering.PromptTokensDetails    `json:"prompt_tokens_details,omitempty"`
	CompletionTokensDetails *metering.CompletionTokensDetails `json:"completion_tokens_details,omitempty"`
}

type GroqMetadata struct {
	ID string `json:"id,omitempty"`
}

type ReveniumGroq struct {
	config   *Config
	provider Provider
	mu       sync.RWMutex
	client   *http.Client
	metering *metering.MeteringClient
}

var (
	globalClient *ReveniumGroq
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
	core.Info("Initializing Revenium middleware for Groq...")

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

	globalClient = &ReveniumGroq{
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

func GetClient() (*ReveniumGroq, error) {
	globalMu.RLock()
	defer globalMu.RUnlock()

	if !initialized {
		return nil, core.NewConfigError("middleware not initialized, call Initialize() first", nil)
	}

	return globalClient, nil
}

func NewReveniumGroq(cfg *Config) (*ReveniumGroq, error) {
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

	return &ReveniumGroq{
		config:   cfg,
		provider: provider,
		client: &http.Client{
			Timeout: 60 * time.Second,
		},
		metering: mc,
	}, nil
}

func (r *ReveniumGroq) GetConfig() *Config {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.config
}

func (r *ReveniumGroq) GetProvider() Provider {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.provider
}

func (r *ReveniumGroq) ChatCompletions(ctx context.Context, req ChatCompletionRequest) (*ChatCompletionResponse, error) {
	metadata := core.GetUsageMetadata(ctx)
	startTime := time.Now()

	resp, err := r.callGroqAPI(ctx, req)
	if err != nil {
		return nil, err
	}

	duration := time.Since(startTime)

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

	payload := metering.NewPayload(metering.OperationChat, resp.Model, "Groq").
		WithTiming(startTime, duration).
		WithTokens(int64(resp.Usage.PromptTokens), int64(resp.Usage.CompletionTokens), int64(resp.Usage.TotalTokens)).
		WithReasoningTokens(int64(reasoningTokens), 0, int64(cacheReadTokens)).
		WithStopReason(stopReason).
		WithModelSource("GROQ").
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

func (r *ReveniumGroq) callGroqAPI(ctx context.Context, req ChatCompletionRequest) (*ChatCompletionResponse, error) {
	baseURL := r.config.BaseURL
	if baseURL == "" {
		baseURL = "https://api.groq.com/openai/v1"
	}
	url := baseURL + "/chat/completions"

	jsonData, err := json.Marshal(req)
	if err != nil {
		return nil, core.NewProviderError("failed to marshal request", err)
	}

	core.Debug("Calling Groq API: %s", url)

	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, core.NewProviderError("failed to create request", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	if r.config.GroqAPIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+r.config.GroqAPIKey)
	}

	httpResp, err := r.client.Do(httpReq)
	if err != nil {
		return nil, core.NewNetworkError("Groq API request failed", err)
	}
	defer httpResp.Body.Close()

	body, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, core.NewNetworkError("failed to read response body", err)
	}

	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		return nil, core.NewProviderError(
			fmt.Sprintf("Groq API returned %d: %s", httpResp.StatusCode, string(body)),
			nil,
		)
	}

	var resp ChatCompletionResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, core.NewProviderError("failed to parse response", err)
	}

	return &resp, nil
}

func (r *ReveniumGroq) Flush() {
	r.metering.Flush()
}

func (r *ReveniumGroq) Close() error {
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
