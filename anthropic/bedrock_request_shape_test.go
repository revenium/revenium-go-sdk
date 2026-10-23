package anthropic

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"

	anthropicsdk "github.com/anthropics/anthropic-sdk-go"
	brtypes "github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const redactedThinkingData = "RW5jcnlwdGVkIHJlYXNvbmluZw=="

func withAnthropicOnlyFields(p anthropicsdk.MessageNewParams) anthropicsdk.MessageNewParams {
	p.Metadata = anthropicsdk.MetadataParam{UserID: anthropicsdk.String("user-1")}
	p.ServiceTier = anthropicsdk.MessageNewParamsServiceTierStandardOnly
	return p
}

func TestBedrockLeavesOutAnthropicOnlyFields(t *testing.T) {
	for _, useConverse := range []bool{false, true} {
		t.Run(map[bool]string{false: "InvokeModel", true: "Converse"}[useConverse], func(t *testing.T) {
			stub := newBedrockStub(t)
			stub.respondWith(invokeModelAnswer)
			stub.streamWith(invokeStreamChunks(invokeStreamMessageStartWithoutCache, invokeStreamBlockDelta, invokeStreamOutputOnly, invokeStreamStopWithoutCache))
			if useConverse {
				stub.respondWith(converseAnswer("end_turn", `{"inputTokens":10,"outputTokens":6,"totalTokens":16}`))
				stub.streamWith(converseStopEvents("end_turn"))
			}
			cfg := bedrockConfig(newMeteringMock(t).URL())
			cfg.BedrockUseConverse = useConverse
			r := newBedrockAnthropic(t, cfg)
			params := withAnthropicOnlyFields(messageParams("claude-haiku-4-5-20251001"))

			_, err := r.Messages().CreateMessage(context.Background(), params)
			require.NoError(t, err)
			stream, err := r.Messages().CreateMessageStream(context.Background(), params)
			require.NoError(t, err)
			drainStream(t, stream)

			requests := stub.received()
			require.Len(t, requests, 2)
			for _, request := range requests {
				body := request.jsonBody(t)
				assert.NotContains(t, body, "metadata")
				assert.NotContains(t, body, "service_tier")
			}
		})
	}
}

func TestBedrockInvokeModelRefusesAFieldItsBodyHasNoRoomFor(t *testing.T) {
	stub := newBedrockStub(t)
	anthropicAPI := newProviderStub(t)
	t.Setenv("ANTHROPIC_BASE_URL", anthropicAPI.server.URL)
	r := newBedrockAnthropic(t, bedrockConfig(newMeteringMock(t).URL()))
	params := messageParams("claude-haiku-4-5-20251001")
	params.InferenceGeo = anthropicsdk.String("us")

	_, err := r.Messages().CreateMessage(context.Background(), params)
	require.ErrorIs(t, err, ErrBedrockCannotCarry)
	assert.Contains(t, err.Error(), "inference_geo on InvokeModel")
	_, err = r.Messages().CreateMessageStream(context.Background(), params)
	require.ErrorIs(t, err, ErrBedrockCannotCarry)

	assert.Empty(t, stub.received())
	assert.Zero(t, anthropicAPI.hits.Load())
}

func TestBedrockRefusalIsNotRetriedAndFallsBackOnceWhenAskedFor(t *testing.T) {
	stub := newBedrockStub(t)
	anthropicAPI := newProviderStub(t)
	t.Setenv("ANTHROPIC_BASE_URL", anthropicAPI.server.URL)
	cfg := bedrockConfig(newMeteringMock(t).URL())
	cfg.BedrockUseConverse = true
	WithBedrockFallbackToAnthropic(true)(cfg)
	r := newBedrockAnthropic(t, cfg)
	params := messageParams("claude-haiku-4-5-20251001")
	params.InferenceGeo = anthropicsdk.String("us")

	started := time.Now()
	_, err := r.Messages().CreateMessage(context.Background(), params)
	require.NoError(t, err)

	assert.Less(t, time.Since(started), 900*time.Millisecond, "a refusal must not wait out retry delays")
	assert.Empty(t, stub.received())
	assert.Equal(t, int32(1), anthropicAPI.hits.Load())
}

func TestBedrockConverseMarksAFailedToolResult(t *testing.T) {
	stub := newBedrockStub(t)
	stub.respondWith(converseToolUseReply)
	r := newConverseAnthropic(t, newMeteringMock(t).URL())
	params := messageParams("claude-haiku-4-5-20251001")
	params.Messages = []anthropicsdk.MessageParam{
		anthropicsdk.NewUserMessage(anthropicsdk.NewTextBlock("weather?")),
		anthropicsdk.NewAssistantMessage(anthropicsdk.NewToolUseBlock("tooluse_1", map[string]any{"city": "Paris"}, "get_weather")),
		anthropicsdk.NewUserMessage(anthropicsdk.NewToolResultBlock("tooluse_1", "service down", true)),
	}

	_, err := r.Messages().CreateMessage(context.Background(), params)
	require.NoError(t, err)

	messages := stub.onlyRequest(t).jsonBody(t)["messages"].([]interface{})
	toolResult := messages[2].(map[string]interface{})["content"].([]interface{})[0].(map[string]interface{})["toolResult"].(map[string]interface{})
	assert.Equal(t, "error", toolResult["status"])
}

func TestBedrockConverseCarriesRedactedThinkingBothWays(t *testing.T) {
	stub := newBedrockStub(t)
	stub.respondWith(`{"output":{"message":{"role":"assistant","content":[{"reasoningContent":{"redactedContent":"` + redactedThinkingData + `"}},` +
		`{"text":"Done."}]}},"stopReason":"end_turn","usage":{"inputTokens":10,"outputTokens":6,"totalTokens":16},"metrics":{"latencyMs":5}}`)
	stub.streamWith([]bedrockEvent{
		{eventType: "messageStart", payload: `{"role":"assistant"}`},
		{eventType: "contentBlockDelta", payload: `{"contentBlockIndex":0,"delta":{"reasoningContent":{"redactedContent":"` + redactedThinkingData + `"}}}`},
		{eventType: "messageStop", payload: `{"stopReason":"end_turn"}`},
		{eventType: "metadata", payload: `{"usage":{"inputTokens":10,"outputTokens":6,"totalTokens":16},"metrics":{"latencyMs":5}}`},
	})
	r := newConverseAnthropic(t, newMeteringMock(t).URL())
	params := messageParams("claude-haiku-4-5-20251001")
	params.Messages = append(params.Messages,
		anthropicsdk.NewAssistantMessage(anthropicsdk.NewRedactedThinkingBlock(redactedThinkingData), anthropicsdk.NewTextBlock("Thinking done.")),
		anthropicsdk.NewUserMessage(anthropicsdk.NewTextBlock("go on")))

	resp, err := r.Messages().CreateMessage(context.Background(), params)
	require.NoError(t, err)
	require.Len(t, resp.Content, 2)
	assert.Equal(t, "redacted_thinking", resp.Content[0].Type)
	assert.Equal(t, redactedThinkingData, resp.Content[0].AsRedactedThinking().Data)

	stream, err := r.Messages().CreateMessageStream(context.Background(), params)
	require.NoError(t, err)
	wrapper := stream.(*StreamingWrapper)
	var redacted []byte
	for wrapper.Next() {
		if delta, ok := wrapper.Current().(*brtypes.ConverseStreamOutputMemberContentBlockDelta); ok {
			if reasoning, ok := delta.Value.Delta.(*brtypes.ContentBlockDeltaMemberReasoningContent); ok {
				if content, ok := reasoning.Value.(*brtypes.ReasoningContentBlockDeltaMemberRedactedContent); ok {
					redacted = content.Value
				}
			}
		}
	}
	require.NoError(t, wrapper.Close())
	assert.Equal(t, redactedThinkingData, base64.StdEncoding.EncodeToString(redacted))

	for _, request := range stub.received() {
		assistant := request.jsonBody(t)["messages"].([]interface{})[1].(map[string]interface{})["content"].([]interface{})
		assert.Equal(t, map[string]interface{}{"reasoningContent": map[string]interface{}{"redactedContent": redactedThinkingData}}, assistant[0])
	}
	assert.Len(t, stub.received(), 2)
}

func TestTransformRequestToBedrockFormatReturnsGoValues(t *testing.T) {
	params := fullBedrockParams()
	params.Messages = append(params.Messages, anthropicsdk.NewAssistantMessage(
		anthropicsdk.NewToolUseBlock("tooluse_1", map[string]any{"order_id": int64(9007199254740993)}, "lookup_order")))

	body := (&BedrockAdapter{}).TransformRequestToBedrockFormat(params)

	require.NotNil(t, body)
	assert.Equal(t, int64(2048), body["max_tokens"])
	assert.Equal(t, 0.25, body["temperature"])
	assert.Equal(t, "bedrock-2023-05-31", body["anthropic_version"])
	toolUse := body["messages"].([]interface{})[1].(map[string]interface{})["content"].([]interface{})[0].(map[string]interface{})
	assert.Equal(t, int64(9007199254740993), toolUse["input"].(map[string]interface{})["order_id"])
}

func TestTransformRequestToBedrockFormatKeepsIntegersBeyondInt64Exact(t *testing.T) {
	params := fullBedrockParams()
	params.Messages = append(params.Messages, anthropicsdk.NewAssistantMessage(
		anthropicsdk.NewToolUseBlock("tooluse_1", map[string]any{"order_id": uint64(18446744073709551615)}, "lookup_order")))

	body := (&BedrockAdapter{}).TransformRequestToBedrockFormat(params)

	require.NotNil(t, body)
	toolUse := body["messages"].([]interface{})[1].(map[string]interface{})["content"].([]interface{})[0].(map[string]interface{})
	assert.Equal(t, json.Number("18446744073709551615"), toolUse["input"].(map[string]interface{})["order_id"])
}

func TestBedrockFallbackSettingInCodeWinsOverTheEnvironment(t *testing.T) {
	cases := []struct {
		name     string
		env      string
		options  []Option
		expected bool
	}{
		{name: "code turns it off while the environment turns it on", env: "true", options: []Option{WithBedrockFallbackToAnthropic(false)}, expected: false},
		{name: "code turns it on while the environment leaves it off", env: "false", options: []Option{WithBedrockFallbackToAnthropic(true)}, expected: true},
		{name: "the environment alone turns it on", env: "true", expected: true},
		{name: "nothing turns it on", env: "", expected: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("REVENIUM_BEDROCK_FALLBACK_TO_ANTHROPIC", tc.env)
			cfg := &Config{}
			for _, option := range tc.options {
				option(cfg)
			}
			require.NoError(t, cfg.loadFromEnv())
			assert.Equal(t, tc.expected, cfg.BedrockFallbackToAnthropic)
		})
	}
}
