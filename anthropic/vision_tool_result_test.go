package anthropic

import (
	"context"
	"testing"

	anthropicsdk "github.com/anthropics/anthropic-sdk-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var visionAttributes = map[string]interface{}{"hasVision": true}

func screenshotToolTurn(content ...anthropicsdk.ToolResultBlockParamContentUnion) []anthropicsdk.MessageParam {
	result := anthropicsdk.ToolResultBlockParam{ToolUseID: "toolu_1", Content: content}
	return []anthropicsdk.MessageParam{
		anthropicsdk.NewUserMessage(anthropicsdk.NewTextBlock("what is on my screen?")),
		anthropicsdk.NewAssistantMessage(anthropicsdk.NewToolUseBlock("toolu_1", map[string]any{}, "screenshot")),
		anthropicsdk.NewUserMessage(anthropicsdk.ContentBlockParamUnion{OfToolResult: &result}),
	}
}

func screenshotResult() anthropicsdk.ToolResultBlockParamContentUnion {
	return anthropicsdk.ToolResultBlockParamContentUnion{OfImage: anthropicsdk.NewImageBlockBase64("image/png", onePixelPNG).OfImage}
}

func textResult() anthropicsdk.ToolResultBlockParamContentUnion {
	return anthropicsdk.ToolResultBlockParamContentUnion{OfText: &anthropicsdk.TextBlockParam{Text: "42"}}
}

func screenshotParams() anthropicsdk.MessageNewParams {
	params := messageParams("claude-haiku-4-5-20251001")
	params.Messages = screenshotToolTurn(textResult(), screenshotResult())
	return params
}

func TestDetectVisionContent_ImageInsideToolResult(t *testing.T) {
	assert.True(t, DetectVisionContent(screenshotToolTurn(textResult(), screenshotResult())))
	assert.False(t, DetectVisionContent(screenshotToolTurn(textResult())))
	assert.False(t, DetectVisionContent(screenshotToolTurn()))
}

func TestDetectBetaVisionContent_ImageInsideToolResult(t *testing.T) {
	withImage := anthropicsdk.BetaToolResultBlockParam{ToolUseID: "toolu_1", Content: []anthropicsdk.BetaToolResultBlockParamContentUnion{
		{OfImage: anthropicsdk.NewBetaImageBlock(anthropicsdk.BetaBase64ImageSourceParam{MediaType: "image/png", Data: onePixelPNG}).OfImage},
	}}
	textOnly := anthropicsdk.BetaToolResultBlockParam{ToolUseID: "toolu_1", Content: []anthropicsdk.BetaToolResultBlockParamContentUnion{
		{OfText: &anthropicsdk.BetaTextBlockParam{Text: "42"}},
	}}

	assert.True(t, detectBetaVisionContent([]anthropicsdk.BetaMessageParam{
		anthropicsdk.NewBetaUserMessage(anthropicsdk.BetaContentBlockParamUnion{OfToolResult: &withImage}),
	}))
	assert.False(t, detectBetaVisionContent([]anthropicsdk.BetaMessageParam{
		anthropicsdk.NewBetaUserMessage(anthropicsdk.BetaContentBlockParamUnion{OfToolResult: &textOnly}),
	}))
}

func TestToolResultImageSetsVisionFlag_DirectMessages(t *testing.T) {
	newBetaProviderStub(t, betaFinalMessage)
	r, mock := newMeteredAnthropic(t)

	_, err := r.Messages().CreateMessage(context.Background(), screenshotParams())
	require.NoError(t, err)
	assert.Equal(t, visionAttributes, onlyRecord(t, r, mock)["attributes"])

	mock.Reset()
	stream, err := r.Messages().CreateMessageStream(context.Background(), screenshotParams())
	require.NoError(t, err)
	drainStream(t, stream)
	assert.Equal(t, visionAttributes, onlyRecord(t, r, mock)["attributes"])
}

func TestToolResultImageSetsVisionFlag_BetaMessages(t *testing.T) {
	newBetaProviderStub(t, betaFinalMessage)
	r, mock := newMeteredAnthropic(t)
	result := anthropicsdk.BetaToolResultBlockParam{ToolUseID: "toolu_1", Content: []anthropicsdk.BetaToolResultBlockParamContentUnion{
		{OfImage: anthropicsdk.NewBetaImageBlock(anthropicsdk.BetaBase64ImageSourceParam{MediaType: "image/png", Data: onePixelPNG}).OfImage},
	}}
	params := betaParams()
	params.Messages = append(params.Messages,
		anthropicsdk.BetaMessageParam{Role: anthropicsdk.BetaMessageParamRoleAssistant, Content: []anthropicsdk.BetaContentBlockParamUnion{
			anthropicsdk.NewBetaToolUseBlock("toolu_1", map[string]any{}, "screenshot"),
		}},
		anthropicsdk.NewBetaUserMessage(anthropicsdk.BetaContentBlockParamUnion{OfToolResult: &result}),
	)

	_, err := r.BetaMessages().CreateMessage(context.Background(), params)
	require.NoError(t, err)
	assert.Equal(t, visionAttributes, onlyRecord(t, r, mock)["attributes"])

	mock.Reset()
	stream, err := r.BetaMessages().CreateMessageStream(context.Background(), params)
	require.NoError(t, err)
	for stream.Next() {
		_ = stream.Current()
	}
	require.NoError(t, stream.Close())
	assert.Equal(t, visionAttributes, onlyRecord(t, r, mock)["attributes"])
}

func TestToolResultImageSetsVisionFlag_BedrockInvokeModel(t *testing.T) {
	stub := newBedrockStub(t)
	stub.respondWith(invokeModelAnswer)
	mock := newMeteringMock(t)
	r := newBedrockAnthropic(t, bedrockConfig(mock.URL()))

	_, err := r.Messages().CreateMessage(context.Background(), screenshotParams())
	require.NoError(t, err)

	assert.Contains(t, string(stub.onlyRequest(t).body), onePixelPNG)
	assert.Equal(t, visionAttributes, onlyRecord(t, r, mock)["attributes"])
}

func TestToolResultImageSetsVisionFlag_BedrockConverse(t *testing.T) {
	stub := newBedrockStub(t)
	stub.respondWith(converseToolUseReply)
	mock := newMeteringMock(t)
	r := newConverseAnthropic(t, mock.URL())

	_, err := r.Messages().CreateMessage(context.Background(), screenshotParams())
	require.NoError(t, err)

	assert.Contains(t, string(stub.onlyRequest(t).body), onePixelPNG)
	assert.Equal(t, visionAttributes, onlyRecord(t, r, mock)["attributes"])
}
