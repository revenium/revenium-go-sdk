package grok

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMapStopReasonToRevenium(t *testing.T) {
	cases := map[string]string{
		"stop":           "END",
		"length":         "TOKEN_LIMIT",
		"tool_calls":     "END",
		"function_call":  "END",
		"content_filter": "ERROR",
		"null":           "END",
		"":               "END",
		"unknown":        "END",
	}
	for in, want := range cases {
		t.Run(in, func(t *testing.T) {
			assert.Equal(t, want, mapStopReasonToRevenium(in))
		})
	}
}

func TestHasVisionContent_DetectsImageURLPart(t *testing.T) {
	msgs := []ChatMessage{
		{Role: "user", Content: []interface{}{
			map[string]interface{}{"type": "image_url", "image_url": map[string]interface{}{"url": "data:image/png;base64,abc"}},
		}},
	}
	assert.True(t, hasVisionContent(msgs))
}

func TestHasVisionContent_DetectsImageReferenceInString(t *testing.T) {
	cases := map[string]bool{
		"plain text":                 false,
		"check out photo.jpg please": true,
		"https://x/img.png":          true,
		"data:image/jpeg;base64,abc": true,
		"a .webp here":               true,
	}
	for content, want := range cases {
		t.Run(content, func(t *testing.T) {
			msgs := []ChatMessage{{Role: "user", Content: content}}
			assert.Equal(t, want, hasVisionContent(msgs))
		})
	}
}

func TestHasVisionContent_NoMessages(t *testing.T) {
	assert.False(t, hasVisionContent(nil))
	assert.False(t, hasVisionContent([]ChatMessage{}))
}
