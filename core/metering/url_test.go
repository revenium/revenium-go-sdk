package metering

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMeteringEndpoint(t *testing.T) {
	tests := []struct {
		name     string
		baseURL  string
		op       OperationType
		expected string
	}{
		{"chat default", "", OperationChat, "https://api.revenium.ai/meter/v2/ai/completions"},
		{"vision default", "", OperationVision, "https://api.revenium.ai/meter/v2/ai/completions"},
		{"embed default", "", OperationEmbed, "https://api.revenium.ai/meter/v2/ai/completions"},
		{"image default", "", OperationImage, "https://api.revenium.ai/meter/v2/ai/images"},
		{"video default", "", OperationVideo, "https://api.revenium.ai/meter/v2/ai/video"},
		{"audio default", "", OperationAudio, "https://api.revenium.ai/meter/v2/ai/audio"},
		{"custom base", "https://custom.api.com", OperationChat, "https://custom.api.com/meter/v2/ai/completions"},
		{"trailing slash", "https://custom.api.com/", OperationChat, "https://custom.api.com/meter/v2/ai/completions"},
		{"legacy meter/v2", "https://custom.api.com/meter/v2", OperationChat, "https://custom.api.com/meter/v2/ai/completions"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, MeteringEndpoint(tt.baseURL, tt.op))
		})
	}
}
