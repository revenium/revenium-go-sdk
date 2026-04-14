package ollama

import "context"

// Client interface for testing and mocking
type Client interface {
	ChatCompletions(ctx context.Context, req ChatCompletionRequest) (*ChatCompletionResponse, error)
	Close() error
}

// Ensure ReveniumOllama implements the Client interface
var _ Client = (*ReveniumOllama)(nil)
