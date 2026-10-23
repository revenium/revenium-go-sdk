package core

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestWithDefaultUsageMetadata_FillsKeysTheCallLeavesUnsetOrBlank(t *testing.T) {
	ctx := WithDefaultUsageMetadata(context.Background(), map[string]interface{}{"agenticJobId": "job-1", "agenticJobName": "nightly", "ticketId": "ENG-1"})
	ctx = WithUsageMetadata(ctx, map[string]interface{}{"agenticJobName": "  ", "ticketId": "ENG-2", "agent": "bot"})

	assert.Equal(t, map[string]interface{}{
		"agenticJobId":   "job-1",
		"agenticJobName": "nightly",
		"ticketId":       "ENG-2",
		"agent":          "bot",
	}, GetUsageMetadata(ctx))
}

func TestWithDefaultUsageMetadata_KeepsTheParentsDefaults(t *testing.T) {
	ctx := WithDefaultUsageMetadata(context.Background(), map[string]interface{}{"ticketId": "ENG-1"})
	ctx = WithDefaultUsageMetadata(ctx, map[string]interface{}{"agenticJobId": "job-1"})

	assert.Equal(t, map[string]interface{}{"ticketId": "ENG-1", "agenticJobId": "job-1"}, GetUsageMetadata(ctx))
}

func TestGetUsageMetadata_WithoutDefaultsReturnsTheStoredMetadata(t *testing.T) {
	stored := map[string]interface{}{"agent": "bot"}

	assert.Equal(t, stored, GetUsageMetadata(WithUsageMetadata(context.Background(), stored)))
}

func TestWithDefaultUsageMetadata_IgnoresBlankDefaults(t *testing.T) {
	ctx := WithDefaultUsageMetadata(context.Background(), map[string]interface{}{"agenticJobId": "job-1", "agenticJobName": " "})

	assert.Equal(t, map[string]interface{}{"agenticJobId": "job-1"}, GetUsageMetadata(ctx))
}

func TestUsageMetadataOver_ResolvesDefaultsThenArgumentThenContext(t *testing.T) {
	ctx := WithDefaultUsageMetadata(context.Background(), map[string]interface{}{"agenticJobId": "from-default", "agenticJobName": "default-name", "ticketId": "ENG-1"})
	ctx = WithUsageMetadata(ctx, map[string]interface{}{"agenticJobName": "", "agent": "from-context"})

	metadata := UsageMetadataOver(ctx, map[string]interface{}{"agenticJobId": "from-argument", "agenticJobName": "argument-name", "agent": "from-argument"})

	assert.Equal(t, map[string]interface{}{
		"agenticJobId":   "from-argument",
		"agenticJobName": "argument-name",
		"ticketId":       "ENG-1",
		"agent":          "from-context",
	}, metadata)
}

func TestWithoutDefaultUsageMetadata_RemovesOnlyTheNamedKeys(t *testing.T) {
	ctx := WithDefaultUsageMetadata(context.Background(), map[string]interface{}{"agenticJobId": "job-1", "ticketId": "ENG-1"})

	assert.Equal(t, map[string]interface{}{"ticketId": "ENG-1"}, GetUsageMetadata(WithoutDefaultUsageMetadata(ctx, "agenticJobId")))
}
