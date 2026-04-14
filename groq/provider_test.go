package groq

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDetectProvider(t *testing.T) {
	assert.Equal(t, ProviderGroq, DetectProvider(nil))
	assert.Equal(t, ProviderGroq, DetectProvider(&Config{}))
}

func TestProvider_IsGroq(t *testing.T) {
	assert.True(t, ProviderGroq.IsGroq())
	assert.False(t, Provider("OTHER").IsGroq())
}

func TestProvider_String(t *testing.T) {
	assert.Equal(t, "GROQ", ProviderGroq.String())
}
