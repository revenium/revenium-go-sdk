package ollama

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
)

// ChatCompletionRequest represents the OpenAI-compatible chat completion request
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
	ResponseFormat   map[string]interface{} `json:"response_format,omitempty"`
	Seed             *int                   `json:"seed,omitempty"`
}

// ChatMessage represents a message in the chat
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
	Name    string `json:"name,omitempty"`
}

// ChatCompletionResponse represents the response from Ollama API
type ChatCompletionResponse struct {
	ID                string   `json:"id"`
	Object            string   `json:"object"`
	Created           int64    `json:"created"`
	Model             string   `json:"model"`
	Choices           []Choice `json:"choices"`
	Usage             Usage    `json:"usage"`
	SystemFingerprint string   `json:"system_fingerprint,omitempty"`
}

// Choice represents a completion choice
type Choice struct {
	Index        int         `json:"index"`
	Message      ChatMessage `json:"message"`
	FinishReason string      `json:"finish_reason"`
	Logprobs     interface{} `json:"logprobs,omitempty"`
}

// Usage represents token usage
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type ReveniumOllama struct {
	config   *Config
	provider Provider
	mu       sync.RWMutex
	client   *http.Client
	metering *metering.MeteringClient
	enforcer *enforcement.Engine
}

var sharedClient = core.NewSharedClient("NewReveniumOllama", func(c *ReveniumOllama) *core.ReveniumConfig { return c.config.Revenium })

// Initialize sets up the global Revenium middleware with configuration
func Initialize(opts ...Option) error {
	cfg := &Config{}
	for _, opt := range opts {
		opt(cfg)
	}
	return sharedClient.Initialize(core.NamedAccount(cfg.Revenium), func() (*ReveniumOllama, error) {
		return buildSharedClient(cfg)
	})
}

func buildSharedClient(cfg *Config) (*ReveniumOllama, error) {
	core.InitializeLogger()
	core.Info("Initializing Revenium middleware for Ollama...")

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

	candidate := &ReveniumOllama{
		config:   cfg,
		provider: provider,
		client: &http.Client{
			Timeout: 120 * time.Second,
		},
		metering: mc,
		enforcer: enforcement.StartFor(cfg.Revenium),
	}

	core.Info("Revenium middleware initialized successfully (Ollama at %s)", cfg.BaseURL)
	return candidate, nil
}

// IsInitialized checks if the middleware is properly initialized
func IsInitialized() bool {
	return sharedClient.IsInitialized()
}

// GetClient returns the global Revenium client
func GetClient() (*ReveniumOllama, error) {
	return sharedClient.Get()
}

// NewReveniumOllama creates a new Revenium client with explicit configuration
func NewReveniumOllama(cfg *Config) (*ReveniumOllama, error) {
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

	return &ReveniumOllama{
		config:   cfg,
		provider: provider,
		client: &http.Client{
			Timeout: 120 * time.Second,
		},
		metering: mc,
		enforcer: enforcement.StartFor(cfg.Revenium),
	}, nil
}

// GetConfig returns the configuration
func (r *ReveniumOllama) GetConfig() *Config {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.config
}

// GetProvider returns the detected provider
func (r *ReveniumOllama) GetProvider() Provider {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.provider
}

func (r *ReveniumOllama) ChatCompletions(ctx context.Context, req ChatCompletionRequest) (*ChatCompletionResponse, error) {
	if req.Stream {
		return nil, ErrStreamingNotSupported
	}

	if err := r.enforcer.Check(enforcement.NewEvalContext(ctx, req.Model, "Ollama")); err != nil {
		core.Debug("ChatCompletions blocked by enforcement: %v", err)
		return nil, err
	}

	metadata := core.GetUsageMetadata(ctx)
	startTime := time.Now()

	resp, err := r.callOllamaAPI(ctx, req)
	if err != nil {
		r.recordFailure(req.Model, startTime, err, metadata)
		return nil, err
	}

	duration := time.Since(startTime)

	stopReason := "END"
	if len(resp.Choices) > 0 && resp.Choices[0].FinishReason != "" {
		stopReason = mapStopReasonToRevenium(resp.Choices[0].FinishReason)
	}

	payload := metering.NewPayload(metering.OperationChat, resp.Model, "Ollama").
		WithTiming(startTime, duration).
		WithTokens(int64(resp.Usage.PromptTokens), int64(resp.Usage.CompletionTokens), int64(resp.Usage.TotalTokens)).
		WithStopReason(stopReason).
		WithModelSource("OLLAMA").
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

func (r *ReveniumOllama) recordFailure(model string, startTime time.Time, err error, metadata map[string]interface{}) {
	payload := metering.NewPayload(metering.OperationChat, model, "Ollama").
		WithTiming(startTime, time.Since(startTime)).
		WithModelSource("OLLAMA").
		Build()
	metering.ApplyFailedCall(payload, metering.FailureOf(err), metadata)
	r.metering.Send(payload)
}

// callOllamaAPI calls the Ollama API with the given request
func (r *ReveniumOllama) callOllamaAPI(ctx context.Context, req ChatCompletionRequest) (*ChatCompletionResponse, error) {
	baseURL := r.config.BaseURL
	if baseURL == "" {
		baseURL = "http://localhost:11434/v1"
	}
	url := baseURL + "/chat/completions"

	jsonData, err := json.Marshal(req)
	if err != nil {
		return nil, core.NewProviderError("failed to marshal request", err)
	}

	core.Debug("Calling Ollama API: %s", url)

	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, core.NewProviderError("failed to create request", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	// Note: Ollama doesn't require authentication for local use

	httpResp, err := r.client.Do(httpReq)
	if err != nil {
		return nil, core.NewNetworkError("Ollama API request failed (is Ollama running?)", err)
	}
	defer httpResp.Body.Close()

	body, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, core.NewNetworkError("failed to read response body", err)
	}

	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		return nil, core.NewProviderError(
			fmt.Sprintf("Ollama API returned %d: %s", httpResp.StatusCode, string(body)),
			nil,
		).WithProviderAnswer(httpResp.StatusCode, body)
	}

	var resp ChatCompletionResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, core.NewProviderError("failed to parse response", err)
	}

	return &resp, nil
}

func (r *ReveniumOllama) Flush() {
	r.metering.Flush()
}

func (r *ReveniumOllama) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.enforcer.Release()
	return r.metering.Close()
}

// Reset resets the global middleware state for testing
func Reset() {
	_ = sharedClient.ResetAndClose()
}

func completionReply(resp *ChatCompletionResponse) string {
	if len(resp.Choices) == 0 {
		return ""
	}
	return resp.Choices[0].Message.Content
}
