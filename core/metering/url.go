package metering

import "github.com/revenium/revenium-go-sdk/core"

type OperationType string

const (
	OperationChat  OperationType = "CHAT"
	OperationVision OperationType = "VISION"
	OperationImage OperationType = "IMAGE"
	OperationVideo OperationType = "VIDEO"
	OperationAudio OperationType = "AUDIO"
	OperationEmbed OperationType = "EMBED"
)

func ToolEventEndpoint(baseURL string) string {
	base := core.NormalizeReveniumBaseURL(baseURL)
	if base == "" {
		base = core.DefaultReveniumBaseURL
	}
	return base + "/meter/v2/tool/events"
}

func MeteringEndpoint(baseURL string, op OperationType) string {
	base := core.NormalizeReveniumBaseURL(baseURL)
	if base == "" {
		base = core.DefaultReveniumBaseURL
	}

	switch op {
	case OperationImage:
		return base + "/meter/v2/ai/images"
	case OperationVideo:
		return base + "/meter/v2/ai/video"
	case OperationAudio:
		return base + "/meter/v2/ai/audio"
	default:
		return base + "/meter/v2/ai/completions"
	}
}
