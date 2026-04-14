package grok

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDetectProvider(t *testing.T) {
	assert.Equal(t, ProviderXAI, DetectProvider(nil))
	assert.Equal(t, ProviderXAI, DetectProvider(&Config{}))
}

func TestProvider_IsXAI(t *testing.T) {
	assert.True(t, ProviderXAI.IsXAI())
	assert.False(t, Provider("OTHER").IsXAI())
}

func TestProvider_String(t *testing.T) {
	assert.Equal(t, "XAI", ProviderXAI.String())
}
