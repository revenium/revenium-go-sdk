package anthropic

import (
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/stretchr/testify/assert"
)

func TestDetectVisionContent_NoImages(t *testing.T) {
	messages := []anthropic.MessageParam{
		anthropic.NewUserMessage(anthropic.NewTextBlock("Hello")),
	}
	assert.False(t, DetectVisionContent(messages))
}

func TestDetectVisionContent_WithBase64Image(t *testing.T) {
	messages := []anthropic.MessageParam{
		anthropic.NewUserMessage(
			anthropic.NewTextBlock("What is this?"),
			anthropic.NewImageBlockBase64("image/png", "iVBORw0KGgo="),
		),
	}
	assert.True(t, DetectVisionContent(messages))
}

func TestDetectVisionContent_EmptyMessages(t *testing.T) {
	assert.False(t, DetectVisionContent(nil))
	assert.False(t, DetectVisionContent([]anthropic.MessageParam{}))
}

func TestDetectVisionContent_MultipleMessages(t *testing.T) {
	messages := []anthropic.MessageParam{
		anthropic.NewUserMessage(anthropic.NewTextBlock("First message")),
		anthropic.NewAssistantMessage(anthropic.NewTextBlock("Response")),
		anthropic.NewUserMessage(
			anthropic.NewTextBlock("Second with image"),
			anthropic.NewImageBlockBase64("image/jpeg", "data"),
		),
	}
	assert.True(t, DetectVisionContent(messages))
}

func TestDetectVisionContent_OnlyTextBlocks(t *testing.T) {
	messages := []anthropic.MessageParam{
		anthropic.NewUserMessage(
			anthropic.NewTextBlock("Hello"),
			anthropic.NewTextBlock("World"),
		),
		anthropic.NewAssistantMessage(anthropic.NewTextBlock("Hi")),
	}
	assert.False(t, DetectVisionContent(messages))
}
