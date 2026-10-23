package anthropic

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	anthropicsdk "github.com/anthropics/anthropic-sdk-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/revenium/revenium-go-sdk/core"
)

const (
	onePixelPNG          = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg=="
	systemPromptMarker   = "SYSTEM-PROMPT-MARKER: answer in French"
	invokeModelAnswer    = `{"id":"msg_bdrk_1","type":"message","role":"assistant","model":"claude-haiku-4-5-20251001","content":[{"type":"text","text":"Bonjour"}],"stop_reason":"end_turn","usage":{"input_tokens":12,"output_tokens":3}}`
	converseToolUseReply = `{"output":{"message":{"role":"assistant","content":[{"text":"Let me check."},` +
		`{"toolUse":{"toolUseId":"tooluse_1","name":"get_weather","input":{"city":"Paris"}}}]}},` +
		`"stopReason":"tool_use","usage":{"inputTokens":20,"outputTokens":8,"totalTokens":28},"metrics":{"latencyMs":5}}`
)

func fullBedrockParams() anthropicsdk.MessageNewParams {
	return anthropicsdk.MessageNewParams{
		Model:       "claude-haiku-4-5-20251001",
		MaxTokens:   2048,
		System:      []anthropicsdk.TextBlockParam{{Text: systemPromptMarker}},
		Temperature: anthropicsdk.Float(0.25),
		Thinking:    anthropicsdk.ThinkingConfigParamOfEnabled(1024),
		Tools: []anthropicsdk.ToolUnionParam{{OfTool: &anthropicsdk.ToolParam{
			Name:        "get_weather",
			Description: anthropicsdk.String("Weather for a city"),
			InputSchema: anthropicsdk.ToolInputSchemaParam{Properties: map[string]any{"city": map[string]any{"type": "string"}}},
		}}},
		Messages: []anthropicsdk.MessageParam{anthropicsdk.NewUserMessage(
			anthropicsdk.NewTextBlock("describe"),
			anthropicsdk.NewImageBlockBase64("image/png", onePixelPNG),
		)},
	}
}

func onlyRecord(t *testing.T, r *ReveniumAnthropic, mock interface {
	WaitForPayloads(int, time.Duration) bool
	GetPayloads() []map[string]interface{}
}) map[string]interface{} {
	t.Helper()
	r.Flush()
	require.True(t, mock.WaitForPayloads(1, 2*time.Second))
	payloads := mock.GetPayloads()
	require.Len(t, payloads, 1)
	return payloads[0]
}

func TestBedrockInvokeModelSendsTheWholeRequest(t *testing.T) {
	stub := newBedrockStub(t)
	stub.respondWith(invokeModelAnswer)
	mock := newMeteringMock(t)
	r := newBedrockAnthropic(t, bedrockConfig(mock.URL()))

	_, err := r.Messages().CreateMessage(context.Background(), fullBedrockParams())
	require.NoError(t, err)

	request := stub.onlyRequest(t)
	assert.Equal(t, "/model/anthropic.claude-haiku-4-5-20251001/invoke", request.path)
	body := request.jsonBody(t)
	assert.Equal(t, "bedrock-2023-05-31", body["anthropic_version"])
	assert.NotContains(t, body, "model")
	assert.Equal(t, 2048.0, body["max_tokens"])
	assert.Equal(t, 0.25, body["temperature"])
	assert.Equal(t, map[string]interface{}{"type": "enabled", "budget_tokens": 1024.0}, body["thinking"])
	assert.Equal(t, []interface{}{map[string]interface{}{"type": "text", "text": systemPromptMarker}}, body["system"])
	require.Len(t, body["tools"], 1)
	assert.Equal(t, "get_weather", body["tools"].([]interface{})[0].(map[string]interface{})["name"])
	content := body["messages"].([]interface{})[0].(map[string]interface{})["content"].([]interface{})
	require.Len(t, content, 2)
	assert.Equal(t, "image", content[1].(map[string]interface{})["type"])

	record := onlyRecord(t, r, mock)
	assert.Equal(t, "Amazon Bedrock", record["provider"])
	assert.Equal(t, map[string]interface{}{"hasVision": true}, record["attributes"])
}

func TestBedrockConverseSendsTheWholeRequest(t *testing.T) {
	stub := newBedrockStub(t)
	stub.respondWith(converseToolUseReply)
	mock := newMeteringMock(t)
	r := newConverseAnthropic(t, mock.URL())

	resp, err := r.Messages().CreateMessage(context.Background(), fullBedrockParams())
	require.NoError(t, err)

	request := stub.onlyRequest(t)
	assert.Equal(t, "/model/anthropic.claude-haiku-4-5-20251001/converse", request.path)
	body := request.jsonBody(t)
	assert.Equal(t, []interface{}{map[string]interface{}{"text": systemPromptMarker}}, body["system"])
	inference := body["inferenceConfig"].(map[string]interface{})
	assert.Equal(t, 2048.0, inference["maxTokens"])
	assert.InDelta(t, 0.25, inference["temperature"], 1e-6)
	assert.Equal(t, map[string]interface{}{"thinking": map[string]interface{}{"type": "enabled", "budget_tokens": 1024.0}},
		body["additionalModelRequestFields"])
	tool := body["toolConfig"].(map[string]interface{})["tools"].([]interface{})[0].(map[string]interface{})["toolSpec"].(map[string]interface{})
	assert.Equal(t, "get_weather", tool["name"])
	assert.Equal(t, "Weather for a city", tool["description"])
	assert.Equal(t, map[string]interface{}{"city": map[string]interface{}{"type": "string"}},
		tool["inputSchema"].(map[string]interface{})["json"].(map[string]interface{})["properties"])
	content := body["messages"].([]interface{})[0].(map[string]interface{})["content"].([]interface{})
	require.Len(t, content, 2)
	image := content[1].(map[string]interface{})["image"].(map[string]interface{})
	assert.Equal(t, "png", image["format"])
	assert.Equal(t, onePixelPNG, image["source"].(map[string]interface{})["bytes"])

	require.Len(t, resp.Content, 2)
	assert.Equal(t, "Let me check.", resp.Content[0].Text)
	toolUse := resp.Content[1].AsToolUse()
	assert.Equal(t, "tooluse_1", toolUse.ID)
	assert.Equal(t, "get_weather", toolUse.Name)
	assert.JSONEq(t, `{"city":"Paris"}`, string(toolUse.Input))

	record := onlyRecord(t, r, mock)
	assert.Equal(t, map[string]interface{}{"hasVision": true}, record["attributes"])
	assert.Equal(t, "END_SEQUENCE", record["stopReason"])
}

func newConverseAnthropic(t *testing.T, meteringURL string) *ReveniumAnthropic {
	t.Helper()
	cfg := bedrockConfig(meteringURL)
	cfg.BedrockUseConverse = true
	return newBedrockAnthropic(t, cfg)
}

func TestBedrockConverseCarriesToolTurnsTopKAndCachePoints(t *testing.T) {
	stub := newBedrockStub(t)
	stub.respondWith(converseToolUseReply)
	r := newConverseAnthropic(t, newMeteringMock(t).URL())

	_, err := r.Messages().CreateMessage(context.Background(), anthropicsdk.MessageNewParams{
		Model:     "claude-haiku-4-5-20251001",
		MaxTokens: 256,
		System: []anthropicsdk.TextBlockParam{{
			Text:         "long instructions",
			CacheControl: anthropicsdk.CacheControlEphemeralParam{TTL: anthropicsdk.CacheControlEphemeralTTLTTL1h},
		}},
		TopK:       anthropicsdk.Int(40),
		ToolChoice: anthropicsdk.ToolChoiceParamOfTool("get_weather"),
		Tools: []anthropicsdk.ToolUnionParam{{OfTool: &anthropicsdk.ToolParam{
			Name:        "get_weather",
			InputSchema: anthropicsdk.ToolInputSchemaParam{Properties: map[string]any{"city": map[string]any{"type": "string"}}},
		}}},
		Messages: []anthropicsdk.MessageParam{
			anthropicsdk.NewUserMessage(anthropicsdk.NewTextBlock("weather in Paris?")),
			anthropicsdk.NewAssistantMessage(
				anthropicsdk.NewThinkingBlock("sig-1", "the user wants the weather"),
				anthropicsdk.NewToolUseBlock("tooluse_1", map[string]any{"city": "Paris"}, "get_weather"),
			),
			anthropicsdk.NewUserMessage(anthropicsdk.NewToolResultBlock("tooluse_1", "18 degrees", false)),
		},
	})
	require.NoError(t, err)

	body := stub.onlyRequest(t).jsonBody(t)
	assert.Equal(t, []interface{}{
		map[string]interface{}{"text": "long instructions"},
		map[string]interface{}{"cachePoint": map[string]interface{}{"type": "default", "ttl": "1h"}},
	}, body["system"])
	assert.Equal(t, map[string]interface{}{"top_k": 40.0}, body["additionalModelRequestFields"])
	assert.Equal(t, map[string]interface{}{"tool": map[string]interface{}{"name": "get_weather"}},
		body["toolConfig"].(map[string]interface{})["toolChoice"])
	messages := body["messages"].([]interface{})
	require.Len(t, messages, 3)
	assert.Equal(t, []interface{}{
		map[string]interface{}{"reasoningContent": map[string]interface{}{
			"reasoningText": map[string]interface{}{"text": "the user wants the weather", "signature": "sig-1"},
		}},
		map[string]interface{}{"toolUse": map[string]interface{}{
			"toolUseId": "tooluse_1", "name": "get_weather", "input": map[string]interface{}{"city": "Paris"},
		}},
	}, messages[1].(map[string]interface{})["content"])
	assert.Equal(t, []interface{}{
		map[string]interface{}{"toolResult": map[string]interface{}{
			"toolUseId": "tooluse_1", "status": "success",
			"content": []interface{}{map[string]interface{}{"text": "18 degrees"}},
		}},
	}, messages[2].(map[string]interface{})["content"])
}

func TestBedrockConverseRefusesWhatItCannotCarry(t *testing.T) {
	cases := map[string]struct {
		params    anthropicsdk.MessageNewParams
		names     string
		invokeFix bool
	}{
		"server tool": {
			params: func() anthropicsdk.MessageNewParams {
				p := messageParams("claude-haiku-4-5-20251001")
				p.Tools = []anthropicsdk.ToolUnionParam{{OfWebSearchTool20250305: &anthropicsdk.WebSearchTool20250305Param{}}}
				return p
			}(),
			names:     "web_search_20250305 tool on Converse",
			invokeFix: true,
		},
		"image by url": {
			params: func() anthropicsdk.MessageNewParams {
				p := messageParams("claude-haiku-4-5-20251001")
				p.Messages = []anthropicsdk.MessageParam{anthropicsdk.NewUserMessage(anthropicsdk.NewImageBlock(anthropicsdk.URLImageSourceParam{URL: "https://example.com/cat.png"}))}
				return p
			}(),
			names:     "url image source on Converse",
			invokeFix: true,
		},
		"inference geography": {
			params: func() anthropicsdk.MessageNewParams {
				p := messageParams("claude-haiku-4-5-20251001")
				p.InferenceGeo = anthropicsdk.String("us")
				return p
			}(),
			names: "inference_geo on Converse or InvokeModel",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			stub := newBedrockStub(t)
			anthropicAPI := newProviderStub(t)
			t.Setenv("ANTHROPIC_BASE_URL", anthropicAPI.server.URL)
			mock := newMeteringMock(t)
			r := newConverseAnthropic(t, mock.URL())

			_, err := r.Messages().CreateMessage(context.Background(), tc.params)
			require.ErrorIs(t, err, ErrBedrockCannotCarry)
			assert.Contains(t, err.Error(), "bedrock cannot carry this part of the request: "+tc.names)
			if tc.invokeFix {
				assert.Contains(t, err.Error(), "; InvokeModel can carry it: leave WithBedrockUseConverse")
			} else {
				assert.NotContains(t, err.Error(), "InvokeModel can carry it")
			}

			_, err = r.Messages().CreateMessageStream(context.Background(), tc.params)
			require.ErrorIs(t, err, ErrBedrockCannotCarry)

			assert.Empty(t, stub.received())
			assert.Zero(t, anthropicAPI.hits.Load())
			r.Flush()
			assert.Empty(t, mock.GetPayloads())
		})
	}
}

func TestBedrockSendsTheSessionTokenOfTemporaryCredentials(t *testing.T) {
	stub := newBedrockStub(t)
	stub.respondWith(invokeModelAnswer)
	mock := newMeteringMock(t)
	t.Setenv("AWS_ACCESS_KEY_ID", "ASIAEXAMPLETEMPKEY")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "secret")
	t.Setenv("AWS_SESSION_TOKEN", "session-token-from-sso")
	t.Setenv("AWS_REGION", "us-east-1")
	t.Setenv(core.EnvTeamID, "")
	cfg := &Config{Revenium: &core.ReveniumConfig{APIKey: "hak_test_key_123", BaseURL: mock.URL()}}
	require.NoError(t, cfg.loadFromEnv())
	r := newBedrockAnthropic(t, cfg)

	_, err := r.Messages().CreateMessage(context.Background(), messageParams("claude-haiku-4-5-20251001"))
	require.NoError(t, err)

	assert.Equal(t, "session-token-from-sso", stub.onlyRequest(t).securityToken)
	assert.Equal(t, "Amazon Bedrock", onlyRecord(t, r, mock)["provider"])
}

func TestBedrockSessionTokenFromTheEnvironmentIsNotPairedWithAnotherKey(t *testing.T) {
	t.Setenv("AWS_ACCESS_KEY_ID", "ASIAEXAMPLETEMPKEY")
	t.Setenv("AWS_SESSION_TOKEN", "session-token-from-sso")
	cfg := &Config{AWSAccessKeyID: "AKIAPROGRAMMATIC", AWSSecretAccessKey: "secret"}

	require.NoError(t, cfg.loadFromEnv())

	assert.Equal(t, "AKIAPROGRAMMATIC", cfg.AWSAccessKeyID)
	assert.Empty(t, cfg.AWSSessionToken)
}

func TestBedrockFailureIsNotResentToAnthropicUnlessAskedFor(t *testing.T) {
	stub := newBedrockStub(t)
	stub.rejectWith(http.StatusForbidden, "UnrecognizedClientException")
	anthropicAPI := newProviderStub(t)
	t.Setenv("ANTHROPIC_BASE_URL", anthropicAPI.server.URL)
	mock := newMeteringMock(t)
	r := newBedrockAnthropic(t, bedrockConfig(mock.URL()))

	_, err := r.Messages().CreateMessage(context.Background(), messageParams("claude-haiku-4-5-20251001"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "UnrecognizedClientException")

	_, err = r.Messages().CreateMessageStream(context.Background(), messageParams("claude-haiku-4-5-20251001"))
	require.Error(t, err)

	assert.Len(t, stub.received(), 2)
	assert.Zero(t, anthropicAPI.hits.Load())
	r.Flush()
	records := mock.GetPayloads()
	require.Len(t, records, 2, "each failed call is recorded once, as a Bedrock call")
	for _, record := range records {
		assert.Equal(t, "ERROR", record["stopReason"])
		assert.Equal(t, "UnrecognizedClientException (HTTP 403)", record["errorReason"])
		assert.Equal(t, float64(403), record["errorCode"])
	}
}

func TestBedrockFailureIsResentToAnthropicWhenAskedFor(t *testing.T) {
	stub := newBedrockStub(t)
	stub.rejectWith(http.StatusForbidden, "UnrecognizedClientException")
	anthropicAPI := newProviderStub(t)
	t.Setenv("ANTHROPIC_BASE_URL", anthropicAPI.server.URL)
	mock := newMeteringMock(t)
	cfg := bedrockConfig(mock.URL())
	WithBedrockFallbackToAnthropic(true)(cfg)
	r := newBedrockAnthropic(t, cfg)

	_, err := r.Messages().CreateMessage(context.Background(), messageParams("claude-haiku-4-5-20251001"))
	require.NoError(t, err)

	assert.Equal(t, int32(1), anthropicAPI.hits.Load())
	assert.Equal(t, "Anthropic", onlyRecord(t, r, mock)["provider"])
}

func TestBedrockFallbackToAnthropicIsReadFromTheEnvironment(t *testing.T) {
	t.Setenv("REVENIUM_BEDROCK_FALLBACK_TO_ANTHROPIC", "true")
	cfg := &Config{}
	require.NoError(t, cfg.loadFromEnv())
	assert.True(t, cfg.BedrockFallbackToAnthropic)
}

func TestBedrockConverseSendsToolNumbersExactly(t *testing.T) {
	const bigID, decimal = "9007199254740993", "0.30000000000000004441"
	params := anthropicsdk.MessageNewParams{
		Model:     "claude-haiku-4-5-20251001",
		MaxTokens: 256,
		Tools: []anthropicsdk.ToolUnionParam{{OfTool: &anthropicsdk.ToolParam{
			Name: "lookup_order",
			InputSchema: anthropicsdk.ToolInputSchemaParam{
				Properties: json.RawMessage(`{"order_id":{"type":"integer","maximum":` + bigID + `}}`),
			},
		}}},
		Messages: []anthropicsdk.MessageParam{
			anthropicsdk.NewUserMessage(anthropicsdk.NewTextBlock("status of my order?")),
			anthropicsdk.NewAssistantMessage(anthropicsdk.NewToolUseBlock("tooluse_1",
				json.RawMessage(`{"order_id":`+bigID+`,"ratio":`+decimal+`}`), "lookup_order")),
			anthropicsdk.NewUserMessage(anthropicsdk.NewToolResultBlock("tooluse_1", "shipped", false)),
		},
	}
	for _, streamed := range []bool{false, true} {
		t.Run(map[bool]string{false: "sync", true: "stream"}[streamed], func(t *testing.T) {
			stub := newBedrockStub(t)
			stub.respondWith(converseToolUseReply)
			stub.streamWith(converseStopEvents("end_turn"))
			r := newConverseAnthropic(t, newMeteringMock(t).URL())

			if streamed {
				stream, err := r.Messages().CreateMessageStream(context.Background(), params)
				require.NoError(t, err)
				drainStream(t, stream)
			} else {
				_, err := r.Messages().CreateMessage(context.Background(), params)
				require.NoError(t, err)
			}

			sent := string(stub.onlyRequest(t).body)
			assert.Contains(t, sent, `"order_id":`+bigID)
			assert.Contains(t, sent, `"ratio":`+decimal)
			assert.Contains(t, sent, `"maximum":`+bigID)
		})
	}
}
