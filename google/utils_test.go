package google

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMapAspectRatioToResolution(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"1:1", "1024x1024"},
		{"3:4", "768x1024"},
		{"4:3", "1024x768"},
		{"9:16", "576x1024"},
		{"16:9", "1024x576"},
		{"unknown", "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.expected, mapAspectRatioToResolution(tt.input))
		})
	}
}

func TestMapUpscaleFactorToResolution(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"x2", "upscale_x2"},
		{"x4", "upscale_x4"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.expected, mapUpscaleFactorToResolution(tt.input))
		})
	}
}
