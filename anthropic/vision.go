package anthropic

import "github.com/anthropics/anthropic-sdk-go"

func DetectVisionContent(messages []anthropic.MessageParam) bool {
	for _, msg := range messages {
		for _, block := range msg.Content {
			if block.OfImage != nil || toolResultCarriesImage(block.OfToolResult) {
				return true
			}
		}
	}
	return false
}

func toolResultCarriesImage(result *anthropic.ToolResultBlockParam) bool {
	if result == nil {
		return false
	}
	for _, part := range result.Content {
		if part.OfImage != nil {
			return true
		}
	}
	return false
}
