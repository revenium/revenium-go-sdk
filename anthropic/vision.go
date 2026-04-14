package anthropic

import "github.com/anthropics/anthropic-sdk-go"

func DetectVisionContent(messages []anthropic.MessageParam) bool {
	for _, msg := range messages {
		for _, block := range msg.Content {
			if block.OfImage != nil {
				return true
			}
		}
	}
	return false
}
