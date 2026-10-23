package core

import (
	"context"
	"strings"
)

// contextKey is a type for context keys to avoid collisions
type contextKey string

const (
	usageMetadataKey        contextKey = "revenium_usage_metadata"
	defaultUsageMetadataKey contextKey = "revenium_default_usage_metadata"
	subscriberKey           contextKey = "revenium_subscriber"
)

const subscriberMetadataKey = "subscriber"

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

// WithDefaultUsageMetadata returns a context whose calls carry each non-blank
// entry of defaults for every key the call's own usage metadata leaves unset
// or blank, such as the job set with jobs.SetJobContext. Each key is resolved
// on its own: the call's value, then the default, then the environment
// defaults the metering client applies. Defaults set on a parent context are
// kept unless defaults replaces them.
func WithDefaultUsageMetadata(ctx context.Context, defaults map[string]interface{}) context.Context {
	merged := MergeMetadata(nil, defaultUsageMetadata(ctx))
	for key, value := range defaults {
		if !isBlankString(value) {
			merged[key] = value
		}
	}
	return context.WithValue(ctx, defaultUsageMetadataKey, merged)
}

// WithoutDefaultUsageMetadata returns a context with the named default usage
// metadata keys removed.
func WithoutDefaultUsageMetadata(ctx context.Context, keys ...string) context.Context {
	remaining := MergeMetadata(nil, defaultUsageMetadata(ctx))
	for _, key := range keys {
		delete(remaining, key)
	}
	return context.WithValue(ctx, defaultUsageMetadataKey, remaining)
}

// GetUsageMetadata retrieves usage metadata from context, completed with the
// context's default usage metadata and with the typed subscriber applied as
// described on ApplySubscriber.
func GetUsageMetadata(ctx context.Context) map[string]interface{} {
	return UsageMetadataOver(ctx, nil)
}

// UsageMetadataOver returns the usage metadata for a call that also passes
// its own metadata argument: the context's defaults, then argument, then the
// usage metadata set on the context, each later non-blank value winning, with
// the typed subscriber applied as described on ApplySubscriber.
func UsageMetadataOver(ctx context.Context, argument map[string]interface{}) map[string]interface{} {
	stored := storedUsageMetadata(ctx)
	defaults := defaultUsageMetadata(ctx)
	if len(defaults) == 0 && len(argument) == 0 {
		return ApplySubscriber(ctx, stored)
	}
	return ApplySubscriber(ctx, overlay(overlay(MergeMetadata(nil, defaults), argument), stored))
}

func defaultUsageMetadata(ctx context.Context) map[string]interface{} {
	defaults, _ := ctx.Value(defaultUsageMetadataKey).(map[string]interface{})
	return defaults
}

func overlay(base, top map[string]interface{}) map[string]interface{} {
	for key, value := range top {
		if _, set := base[key]; set && isBlankString(value) {
			continue
		}
		base[key] = value
	}
	return base
}

func isBlankString(value interface{}) bool {
	s, ok := value.(string)
	return ok && IsBlank(s)
}

// IsBlank reports whether s is empty or only white space, which the SDK
// treats as a value that was not set.
func IsBlank(s string) bool {
	return strings.TrimSpace(s) == ""
}

func storedUsageMetadata(ctx context.Context) map[string]interface{} {
	if metadata, ok := ctx.Value(usageMetadataKey).(map[string]interface{}); ok {
		return metadata
	}
	return make(map[string]interface{})
}

// WithSubscriber returns a new context with subscriber information
func WithSubscriber(ctx context.Context, subscriber *Subscriber) context.Context {
	return context.WithValue(ctx, subscriberKey, subscriber)
}

// GetSubscriber returns the subscriber a call is attributed to. The typed
// subscriber set with WithSubscriber wins when it carries an ID or Email;
// otherwise the "subscriber" entry of the usage metadata is used. Cost-control
// matching and the usage record both resolve the subscriber this way.
func GetSubscriber(ctx context.Context) *Subscriber {
	typed := typedSubscriber(ctx)
	if typed.identifiesPerson() {
		return typed
	}
	if fromMetadata := subscriberFromMetadata(storedUsageMetadata(ctx)); fromMetadata != nil {
		return fromMetadata
	}
	return typed
}

// ApplySubscriber returns metadata with its "subscriber" entry replaced by the
// typed subscriber from ctx when that subscriber carries an ID or Email. The
// metadata subscriber's credential is kept — it names the key used, not who
// used it, so the typed identity does not supersede it. The input map is never
// modified; it is returned unchanged when there is nothing to apply.
func ApplySubscriber(ctx context.Context, metadata map[string]interface{}) map[string]interface{} {
	typed := typedSubscriber(ctx)
	if !typed.identifiesPerson() {
		return metadata
	}
	applied := MergeMetadata(nil, metadata)
	fields := typed.RecordFields()
	if existing, ok := applied[subscriberMetadataKey].(map[string]interface{}); ok {
		if credential, found := existing["credential"]; found {
			fields["credential"] = credential
		}
	}
	applied[subscriberMetadataKey] = fields
	return applied
}

// RecordFields returns the subscriber in the shape the usage record carries
// under "subscriber". The API key is deliberately left out: it is a secret,
// not an identity, and the record only needs to say who made the call.
func (s *Subscriber) RecordFields() map[string]interface{} {
	fields := make(map[string]interface{})
	if s.ID != "" {
		fields["id"] = s.ID
	}
	if s.Email != "" {
		fields["email"] = s.Email
	}
	return fields
}

func (s *Subscriber) identifiesPerson() bool {
	return s != nil && (s.ID != "" || s.Email != "")
}

func typedSubscriber(ctx context.Context) *Subscriber {
	if subscriber, ok := ctx.Value(subscriberKey).(*Subscriber); ok {
		return subscriber
	}
	return nil
}

func subscriberFromMetadata(metadata map[string]interface{}) *Subscriber {
	fields, ok := metadata[subscriberMetadataKey].(map[string]interface{})
	if !ok {
		return nil
	}
	id, _ := fields["id"].(string)
	email, _ := fields["email"].(string)
	subscriber := &Subscriber{ID: id, Email: email}
	if !subscriber.identifiesPerson() {
		return nil
	}
	return subscriber
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
