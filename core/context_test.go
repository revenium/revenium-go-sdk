package core

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestWithUsageMetadata(t *testing.T) {
	ctx := context.Background()
	metadata := map[string]interface{}{
		"organizationId": "org-123",
		"productId":      "prod-456",
	}

	ctx = WithUsageMetadata(ctx, metadata)
	retrieved := GetUsageMetadata(ctx)

	assert.Equal(t, metadata, retrieved)
}

func TestGetUsageMetadata_Empty(t *testing.T) {
	ctx := context.Background()
	retrieved := GetUsageMetadata(ctx)

	assert.NotNil(t, retrieved)
	assert.Empty(t, retrieved)
}

func TestWithSubscriber(t *testing.T) {
	ctx := context.Background()
	subscriber := &Subscriber{
		ID:     "user-123",
		APIKey: "sk-test-key",
		Email:  "user@example.com",
		Metadata: map[string]interface{}{
			"plan": "premium",
		},
	}

	ctx = WithSubscriber(ctx, subscriber)
	retrieved := GetSubscriber(ctx)

	assert.Equal(t, subscriber, retrieved)
	assert.Equal(t, "user-123", retrieved.ID)
	assert.Equal(t, "sk-test-key", retrieved.APIKey)
	assert.Equal(t, "user@example.com", retrieved.Email)
	assert.Equal(t, "premium", retrieved.Metadata["plan"])
}

func TestGetSubscriber_Nil(t *testing.T) {
	ctx := context.Background()
	retrieved := GetSubscriber(ctx)

	assert.Nil(t, retrieved)
}

func TestMergeMetadata(t *testing.T) {
	tests := []struct {
		name     string
		base     map[string]interface{}
		override map[string]interface{}
		expected map[string]interface{}
	}{
		{
			name: "merge two maps",
			base: map[string]interface{}{
				"key1": "value1",
				"key2": "value2",
			},
			override: map[string]interface{}{
				"key3": "value3",
			},
			expected: map[string]interface{}{
				"key1": "value1",
				"key2": "value2",
				"key3": "value3",
			},
		},
		{
			name: "override existing keys",
			base: map[string]interface{}{
				"key1": "value1",
				"key2": "value2",
			},
			override: map[string]interface{}{
				"key2": "new_value2",
			},
			expected: map[string]interface{}{
				"key1": "value1",
				"key2": "new_value2",
			},
		},
		{
			name:     "nil base",
			base:     nil,
			override: map[string]interface{}{"key1": "value1"},
			expected: map[string]interface{}{"key1": "value1"},
		},
		{
			name:     "nil override",
			base:     map[string]interface{}{"key1": "value1"},
			override: nil,
			expected: map[string]interface{}{"key1": "value1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := MergeMetadata(tt.base, tt.override)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestExtractMetadata(t *testing.T) {
	ctx := context.Background()
	contextMetadata := map[string]interface{}{
		"organizationId": "org-123",
		"productId":      "prod-456",
	}
	ctx = WithUsageMetadata(ctx, contextMetadata)

	paramMetadata := map[string]interface{}{
		"taskType":  "translation",
		"productId": "prod-789", // Override
	}

	result := ExtractMetadata(ctx, paramMetadata)

	expected := map[string]interface{}{
		"organizationId": "org-123",
		"productId":      "prod-789", // Overridden
		"taskType":       "translation",
	}

	assert.Equal(t, expected, result)
}
