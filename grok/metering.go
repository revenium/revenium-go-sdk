package grok

import (
	"strings"

	"github.com/revenium/revenium-go-sdk/core"
)

func hasVisionContent(messages []ChatMessage) bool {
	for _, msg := range messages {
		if contentParts, ok := msg.Content.([]interface{}); ok {
			for _, part := range contentParts {
				if partMap, ok := part.(map[string]interface{}); ok {
					if partType, ok := partMap["type"].(string); ok && partType == "image_url" {
						return true
					}
				}
			}
		}
		if contentStr, ok := msg.Content.(string); ok {
			if containsImageReference(contentStr) {
				return true
			}
		}
	}
	return false
}

func containsImageReference(content string) bool {
	lowerContent := strings.ToLower(content)
	return strings.Contains(lowerContent, "image_url") ||
		strings.Contains(lowerContent, "data:image/") ||
		strings.Contains(lowerContent, ".jpg") ||
		strings.Contains(lowerContent, ".png") ||
		strings.Contains(lowerContent, ".jpeg") ||
		strings.Contains(lowerContent, ".gif") ||
		strings.Contains(lowerContent, ".webp")
}

func mapStopReasonToRevenium(stopReason string) string {
	switch stopReason {
	case "stop":
		return "END"
	case "length":
		return "TOKEN_LIMIT"
	case "tool_calls", "function_call":
		return "END"
	case "content_filter":
		return "ERROR"
	case "null":
		return "END"
	default:
		core.Debug("Unknown stop reason '%s', defaulting to END", stopReason)
		return "END"
	}
}
