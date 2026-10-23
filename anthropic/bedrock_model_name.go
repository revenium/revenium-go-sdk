package anthropic

import (
	"regexp"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
)

const arnFieldCount = 6

var modelNamingARNResource = regexp.MustCompile(`^(?:foundation-model|inference-profile)/(.+)$`)

// recordedBedrockModel is the model a Bedrock call is recorded and checked
// against cost controls under: the Bedrock model id it was sent to, which is
// how the price catalog names Bedrock models (a regional inference profile is
// priced apart from the base model). An ARN is reduced to its resource, the
// model id it names when it names one, so no record carries the AWS account id.
func recordedBedrockModel(sentModelID string) string {
	if !strings.HasPrefix(sentModelID, "arn:") {
		return sentModelID
	}
	fields := strings.SplitN(sentModelID, ":", arnFieldCount)
	if len(fields) < arnFieldCount {
		return sentModelID
	}
	resource := fields[arnFieldCount-1]
	if match := modelNamingARNResource.FindStringSubmatch(resource); match != nil {
		return match[1]
	}
	return resource
}

func (m *MessagesInterface) recordedBedrockModelFor(params anthropic.MessageNewParams) string {
	return recordedBedrockModel(GetBedrockModelID(string(params.Model), m.config))
}
