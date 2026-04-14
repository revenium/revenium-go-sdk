package core

import (
	"context"
)

// contextKey is a type for context keys to avoid collisions
type contextKey string

const (
	usageMetadataKey contextKey = "revenium_usage_metadata"
	subscriberKey    contextKey = "revenium_subscriber"
)

// UsageMetadata represents metadata about API usage
type UsageMetadata struct {
	OrganizationID string                 `json:"organization_id,omitempty"`
	UserID         string                 `json:"user_id,omitempty"`
	SessionID      string                 `json:"session_id,omitempty"`
	TaskType       string                 `json:"task_type,omitempty"`
	Custom         map[string]interface{} `json:"custom,omitempty"`
}

// Subscriber represents a subscriber with credentials
type Subscriber struct {
	ID       string                 `json:"id,omitempty"`
	APIKey   string                 `json:"api_key,omitempty"`
	Email    string                 `json:"email,omitempty"`
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

// WithUsageMetadata returns a new context with usage metadata
func WithUsageMetadata(ctx context.Context, metadata map[string]interface{}) context.Context {
	return context.WithValue(ctx, usageMetadataKey, metadata)
}

// GetUsageMetadata retrieves usage metadata from context
func GetUsageMetadata(ctx context.Context) map[string]interface{} {
	if metadata, ok := ctx.Value(usageMetadataKey).(map[string]interface{}); ok {
		return metadata
	}
	return make(map[string]interface{})
}

// WithSubscriber returns a new context with subscriber information
func WithSubscriber(ctx context.Context, subscriber *Subscriber) context.Context {
	return context.WithValue(ctx, subscriberKey, subscriber)
}

// GetSubscriber retrieves subscriber information from context
func GetSubscriber(ctx context.Context) *Subscriber {
	if subscriber, ok := ctx.Value(subscriberKey).(*Subscriber); ok {
		return subscriber
	}
	return nil
}

// MergeMetadata merges two metadata maps, with the second taking precedence
func MergeMetadata(base, override map[string]interface{}) map[string]interface{} {
	if base == nil {
		base = make(map[string]interface{})
	}

	if override == nil {
		return base
	}

	// Create a copy of base
	merged := make(map[string]interface{})
	for k, v := range base {
		merged[k] = v
	}

	// Override with values from override
	for k, v := range override {
		merged[k] = v
	}

	return merged
}

// ExtractMetadata extracts metadata from context and parameters
func ExtractMetadata(ctx context.Context, paramMetadata map[string]interface{}) map[string]interface{} {
	contextMetadata := GetUsageMetadata(ctx)
	return MergeMetadata(contextMetadata, paramMetadata)
}
