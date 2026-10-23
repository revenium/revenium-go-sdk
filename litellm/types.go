package litellm

// ChatCompletionRequest represents a request to the LiteLLM chat completions endpoint
type ChatCompletionRequest struct {
	Model            string                 `json:"model"`
	Messages         []ChatMessage          `json:"messages"`
	Temperature      *float64               `json:"temperature,omitempty"`
	MaxTokens        *int                   `json:"max_tokens,omitempty"`
	TopP             *float64               `json:"top_p,omitempty"`
	FrequencyPenalty *float64               `json:"frequency_penalty,omitempty"`
	PresencePenalty  *float64               `json:"presence_penalty,omitempty"`
	Stop             interface{}            `json:"stop,omitempty"`
	Stream           bool                   `json:"stream,omitempty"`
	StreamOptions    *StreamOptions         `json:"stream_options,omitempty"`
	Tools            []Tool                 `json:"tools,omitempty"`
	ToolChoice       interface{}            `json:"tool_choice,omitempty"`
	ResponseFormat   interface{}            `json:"response_format,omitempty"`
	Seed             *int                   `json:"seed,omitempty"`
	User             string                 `json:"user,omitempty"`
	Extra            map[string]interface{} `json:"-"`
}

// StreamOptions represents streaming configuration options
type StreamOptions struct {
	IncludeUsage bool `json:"include_usage,omitempty"`
}

// ChatMessage represents a chat message in the request
type ChatMessage struct {
	Role       string     `json:"role"`
	Content    string     `json:"content,omitempty"`
	Name       string     `json:"name,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

// Tool represents a tool definition
type Tool struct {
	Type     string       `json:"type"`
	Function ToolFunction `json:"function"`
}

// ToolFunction represents a function definition within a tool
type ToolFunction struct {
	Name        string      `json:"name"`
	Description string      `json:"description,omitempty"`
	Parameters  interface{} `json:"parameters,omitempty"`
}

// ToolCall represents a tool call in a response
type ToolCall struct {
	ID       string           `json:"id"`
	Type     string           `json:"type"`
	Function ToolCallFunction `json:"function"`
}

// ToolCallFunction represents the function call details
type ToolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// ChatCompletionResponse represents a response from the LiteLLM chat completions endpoint
type ChatCompletionResponse struct {
	ID                string      `json:"id"`
	Object            string      `json:"object"`
	Created           int64       `json:"created"`
	Model             string      `json:"model"`
	Choices           []Choice    `json:"choices"`
	Usage             *TokenUsage `json:"usage,omitempty"`
	SystemFingerprint string      `json:"system_fingerprint,omitempty"`
}

// Choice represents a choice in the response
type Choice struct {
	Index        int             `json:"index"`
	Message      ResponseMessage `json:"message"`
	FinishReason string          `json:"finish_reason"`
}

// ResponseMessage represents the assistant's response message
type ResponseMessage struct {
	Role      string     `json:"role"`
	Content   string     `json:"content"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
}

// TokenUsage represents token usage information
type TokenUsage struct {
	PromptTokens             int64                    `json:"prompt_tokens"`
	CompletionTokens         int64                    `json:"completion_tokens"`
	TotalTokens              int64                    `json:"total_tokens"`
	CacheCreationInputTokens int64                    `json:"cache_creation_input_tokens,omitempty"`
	PromptTokensDetails      *PromptTokensDetails     `json:"prompt_tokens_details,omitempty"`
	CompletionTokensDetails  *CompletionTokensDetails `json:"completion_tokens_details,omitempty"`
	// Cost is the USD cost the upstream provider reported, which the proxy restates here (xAI's cost_in_usd_ticks, for one).
	Cost *float64 `json:"cost,omitempty"`
}

func (u *TokenUsage) CacheCreationTokenCount() int64 {
	if u == nil {
		return 0
	}
	if u.PromptTokensDetails == nil {
		return u.CacheCreationInputTokens
	}
	if u.PromptTokensDetails.CacheCreationTokens > 0 {
		return u.PromptTokensDetails.CacheCreationTokens
	}
	if u.CacheCreationInputTokens > 0 {
		return u.CacheCreationInputTokens
	}
	return u.PromptTokensDetails.CacheWriteTokens
}

// ReportedCost is the provider's own cost for the call, or nil when it reported none.
func (u *TokenUsage) ReportedCost() *float64 {
	if u == nil || u.Cost == nil || *u.Cost < 0 {
		return nil
	}
	return u.Cost
}

// CacheCreationByTTL returns the five-minute and one-hour cache writes the
// provider reported; a bucket it did not report is nil, never 0.
func (u *TokenUsage) CacheCreationByTTL() (fiveMinute, oneHour *int64) {
	if u == nil || u.PromptTokensDetails == nil || u.PromptTokensDetails.CacheCreationTokenDetails == nil {
		return nil, nil
	}
	byTTL := u.PromptTokensDetails.CacheCreationTokenDetails
	return byTTL.Ephemeral5mInputTokens, byTTL.Ephemeral1hInputTokens
}

// PromptTokensDetails contains details about prompt token usage
type PromptTokensDetails struct {
	CachedTokens              int64                      `json:"cached_tokens,omitempty"`
	CacheCreationTokens       int64                      `json:"cache_creation_tokens,omitempty"`
	CacheWriteTokens          int64                      `json:"cache_write_tokens,omitempty"`
	CacheCreationTokenDetails *CacheCreationTokenDetails `json:"cache_creation_token_details,omitempty"`
}

// CacheCreationTokenDetails is Claude's cache write split by time to live, as
// LiteLLM passes it on from Anthropic's usage.cache_creation
type CacheCreationTokenDetails struct {
	Ephemeral5mInputTokens *int64 `json:"ephemeral_5m_input_tokens,omitempty"`
	Ephemeral1hInputTokens *int64 `json:"ephemeral_1h_input_tokens,omitempty"`
}

// CompletionTokensDetails contains details about completion token usage
type CompletionTokensDetails struct {
	ReasoningTokens int64 `json:"reasoning_tokens,omitempty"`
}

// EmbeddingRequest represents a request to the LiteLLM embeddings endpoint
type EmbeddingRequest struct {
	Model          string      `json:"model"`
	Input          interface{} `json:"input"`
	EncodingFormat string      `json:"encoding_format,omitempty"`
	Dimensions     *int        `json:"dimensions,omitempty"`
	User           string      `json:"user,omitempty"`
}

// EmbeddingResponse represents a response from the LiteLLM embeddings endpoint
type EmbeddingResponse struct {
	Object string          `json:"object"`
	Data   []EmbeddingData `json:"data"`
	Model  string          `json:"model"`
	Usage  EmbeddingUsage  `json:"usage"`
}

// EmbeddingData represents a single embedding in the response
type EmbeddingData struct {
	Object    string    `json:"object"`
	Embedding []float64 `json:"embedding"`
	Index     int       `json:"index"`
}

// EmbeddingUsage represents token usage for embedding requests
type EmbeddingUsage struct {
	PromptTokens int64 `json:"prompt_tokens"`
	TotalTokens  int64 `json:"total_tokens"`
}

// StreamChunk represents a single chunk in a streaming response
type StreamChunk struct {
	ID                string         `json:"id,omitempty"`
	Object            string         `json:"object,omitempty"`
	Created           int64          `json:"created,omitempty"`
	Model             string         `json:"model,omitempty"`
	Choices           []StreamChoice `json:"choices,omitempty"`
	Usage             *TokenUsage    `json:"usage,omitempty"`
	SystemFingerprint string         `json:"system_fingerprint,omitempty"`
	XGroq             *XGroqExtras   `json:"x_groq,omitempty"`
}

// XGroqExtras holds Groq-specific fields sometimes returned via LiteLLM passthrough
type XGroqExtras struct {
	Usage *TokenUsage `json:"usage,omitempty"`
}

// StreamChoice represents a choice in a streaming chunk
type StreamChoice struct {
	Index        int         `json:"index"`
	Delta        StreamDelta `json:"delta"`
	FinishReason string      `json:"finish_reason,omitempty"`
}

// StreamDelta represents the delta content in a streaming chunk
type StreamDelta struct {
	Role      string          `json:"role,omitempty"`
	Content   string          `json:"content,omitempty"`
	ToolCalls []ToolCallDelta `json:"tool_calls,omitempty"`
}

// ToolCallDelta represents an incremental tool-call payload delivered through SSE
type ToolCallDelta struct {
	Index    int                    `json:"index"`
	ID       string                 `json:"id,omitempty"`
	Type     string                 `json:"type,omitempty"`
	Function *ToolCallFunctionDelta `json:"function,omitempty"`
}

// ToolCallFunctionDelta represents an incremental function-call payload
type ToolCallFunctionDelta struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

// ProviderPattern describes a provider detection rule based on prefixes and substring patterns
type ProviderPattern struct {
	Source      string
	DisplayName string
	Prefixes    []string
	Patterns    []string
}

// MiddlewareStatus reports the current state of the LiteLLM middleware client
type MiddlewareStatus struct {
	Initialized bool   `json:"initialized"`
	Enabled     bool   `json:"enabled"`
	HasConfig   bool   `json:"hasConfig"`
	ProxyURL    string `json:"proxyUrl,omitempty"`
}
