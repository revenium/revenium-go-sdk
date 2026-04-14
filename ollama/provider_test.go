package ollama

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDetectProvider(t *testing.T) {
	assert.Equal(t, ProviderOllama, DetectProvider(nil))
	assert.Equal(t, ProviderOllama, DetectProvider(&Config{}))
}

func TestProvider_IsOllama(t *testing.T) {
	assert.True(t, ProviderOllama.IsOllama())
	assert.False(t, Provider("OTHER").IsOllama())
}

func TestProvider_String(t *testing.T) {
	assert.Equal(t, "OLLAMA", ProviderOllama.String())
}
