package anthropic

import (
	"context"
	"testing"

	anthropicsdk "github.com/anthropics/anthropic-sdk-go"
	"github.com/aws/aws-sdk-go-v2/aws"
	awsmiddleware "github.com/aws/aws-sdk-go-v2/aws/middleware"
	smithymiddleware "github.com/aws/smithy-go/middleware"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	brtypes "github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetBedrockModelID(t *testing.T) {
	tests := []struct {
		name     string
		model    string
		arnBase  string
		expected string
	}{
		{
			name:     "bare model gets anthropic prefix",
			model:    "claude-3-5-sonnet-20241022",
			expected: "anthropic.claude-3-5-sonnet-20241022",
		},
		{
			name:     "full ARN passes through",
			model:    "arn:aws:bedrock:us-east-1:123456789012:inference-profile/us.anthropic.claude-3-5-sonnet-v1:0",
			expected: "arn:aws:bedrock:us-east-1:123456789012:inference-profile/us.anthropic.claude-3-5-sonnet-v1:0",
		},
		{
			name:     "us.anthropic prefix passes through",
			model:    "us.anthropic.claude-opus-4-5-20251101-v1:0",
			expected: "us.anthropic.claude-opus-4-5-20251101-v1:0",
		},
		{
			name:     "eu.anthropic prefix passes through",
			model:    "eu.anthropic.claude-3-5-sonnet",
			expected: "eu.anthropic.claude-3-5-sonnet",
		},
		{
			name:     "global.anthropic prefix passes through",
			model:    "global.anthropic.claude-opus-4-8",
			expected: "global.anthropic.claude-opus-4-8",
		},
		{
			name:     "anthropic. prefix passes through",
			model:    "anthropic.claude-3-5-sonnet-20241022-v2:0",
			expected: "anthropic.claude-3-5-sonnet-20241022-v2:0",
		},
		{
			name:     "us.anthropic with AWSModelARNBase still passes through",
			model:    "us.anthropic.claude-opus-4-5-20251101-v1:0",
			arnBase:  "arn:aws:bedrock:us-east-1:123456789012",
			expected: "us.anthropic.claude-opus-4-5-20251101-v1:0",
		},
		{
			name:     "bare model with AWSModelARNBase constructs ARN",
			model:    "claude-3-5-sonnet-20241022",
			arnBase:  "arn:aws:bedrock:us-east-1:123456789012",
			expected: "arn:aws:bedrock:us-east-1:123456789012:inference-profile/us.anthropic.claude-3-5-sonnet-20241022-v1:0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var cfg *Config
			if tt.arnBase != "" {
				cfg = &Config{AWSModelARNBase: tt.arnBase}
			}
			result := GetBedrockModelID(tt.model, cfg)
			assert.Equal(t, tt.expected, result)
			assert.LessOrEqual(t, countOccurrences(result, "anthropic."), 1, "must not double-prefix")
		})
	}
}

func countOccurrences(s, sub string) int {
	count := 0
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			count++
		}
	}
	return count
}

type mockBedrockClient struct {
	invokeModelCalls    int
	converseCalls       int
	invokeStreamCalls   int
	converseStreamCalls int

	invokeModelResp    *bedrockruntime.InvokeModelOutput
	converseResp       *bedrockruntime.ConverseOutput
	invokeModelErr     error
	converseErr        error
}

func (m *mockBedrockClient) InvokeModel(_ context.Context, _ *bedrockruntime.InvokeModelInput, _ ...func(*bedrockruntime.Options)) (*bedrockruntime.InvokeModelOutput, error) {
	m.invokeModelCalls++
	return m.invokeModelResp, m.invokeModelErr
}

func (m *mockBedrockClient) InvokeModelWithResponseStream(_ context.Context, _ *bedrockruntime.InvokeModelWithResponseStreamInput, _ ...func(*bedrockruntime.Options)) (*bedrockruntime.InvokeModelWithResponseStreamOutput, error) {
	m.invokeStreamCalls++
	return nil, nil
}

func (m *mockBedrockClient) Converse(_ context.Context, _ *bedrockruntime.ConverseInput, _ ...func(*bedrockruntime.Options)) (*bedrockruntime.ConverseOutput, error) {
	m.converseCalls++
	return m.converseResp, m.converseErr
}

func (m *mockBedrockClient) ConverseStream(_ context.Context, _ *bedrockruntime.ConverseStreamInput, _ ...func(*bedrockruntime.Options)) (*bedrockruntime.ConverseStreamOutput, error) {
	m.converseStreamCalls++
	return nil, nil
}

func resultMetadataWithRequestID(id string) smithymiddleware.Metadata {
	var md smithymiddleware.Metadata
	awsmiddleware.SetRequestIDMetadata(&md, id)
	return md
}

func TestCreateMessageConverse(t *testing.T) {
	mock := &mockBedrockClient{
		converseResp: &bedrockruntime.ConverseOutput{
			Output: &brtypes.ConverseOutputMemberMessage{
				Value: brtypes.Message{
					Role: brtypes.ConversationRoleAssistant,
					Content: []brtypes.ContentBlock{
						&brtypes.ContentBlockMemberText{Value: "Hello from Converse"},
					},
				},
			},
			StopReason: brtypes.StopReasonEndTurn,
			Usage: &brtypes.TokenUsage{
				InputTokens:       aws.Int32(10),
				OutputTokens:      aws.Int32(5),
				TotalTokens:       aws.Int32(15),
				CacheReadInputTokens:  aws.Int32(3),
				CacheWriteInputTokens: aws.Int32(2),
			},
			ResultMetadata: resultMetadataWithRequestID("req-123"),
		},
	}

	adapter := &BedrockAdapter{
		config: &Config{},
		client: mock,
	}

	params := makeTestParams("claude-3-5-sonnet-20241022", "Hello")
	resp, err := adapter.CreateMessageConverse(context.Background(), params)

	require.NoError(t, err)
	assert.Equal(t, 1, mock.converseCalls)
	assert.Equal(t, 0, mock.invokeModelCalls)
	assert.Equal(t, "req-123", resp.ID)
	assert.Equal(t, int64(10), resp.Usage.InputTokens)
	assert.Equal(t, int64(5), resp.Usage.OutputTokens)
	assert.Equal(t, int64(3), resp.Usage.CacheReadInputTokens)
	assert.Equal(t, int64(2), resp.Usage.CacheCreationInputTokens)
}

func TestCreateMessageConverse_MissingRequestID(t *testing.T) {
	mock := &mockBedrockClient{
		converseResp: &bedrockruntime.ConverseOutput{
			Output: &brtypes.ConverseOutputMemberMessage{
				Value: brtypes.Message{
					Role:    brtypes.ConversationRoleAssistant,
					Content: []brtypes.ContentBlock{&brtypes.ContentBlockMemberText{Value: "ok"}},
				},
			},
			StopReason: brtypes.StopReasonEndTurn,
			Usage:      &brtypes.TokenUsage{InputTokens: aws.Int32(1), OutputTokens: aws.Int32(1), TotalTokens: aws.Int32(2)},
		},
	}

	adapter := &BedrockAdapter{config: &Config{}, client: mock}
	resp, err := adapter.CreateMessageConverse(context.Background(), makeTestParams("claude-3-5-sonnet", "hi"))

	require.NoError(t, err)
	assert.Empty(t, resp.ID)
}

func TestCreateMessage_DefaultUsesInvokeModel(t *testing.T) {
	mock := &mockBedrockClient{
		invokeModelResp: &bedrockruntime.InvokeModelOutput{
			Body: []byte(`{"id":"msg-1","model":"claude-3-5-sonnet","content":[{"type":"text","text":"Hello"}],"stop_reason":"end_turn","usage":{"input_tokens":10,"output_tokens":5}}`),
		},
	}

	adapter := &BedrockAdapter{config: &Config{}, client: mock}
	_, err := adapter.CreateMessage(context.Background(), makeTestParams("claude-3-5-sonnet", "Hello"))

	require.NoError(t, err)
	assert.Equal(t, 1, mock.invokeModelCalls)
	assert.Equal(t, 0, mock.converseCalls)
}

func TestDetectProvider_Bedrock_AWSCredentials(t *testing.T) {
	cfg := &Config{AWSAccessKeyID: "key", AWSSecretAccessKey: "secret"}
	assert.Equal(t, ProviderBedrock, DetectProvider(cfg))
}

func TestDetectProvider_Bedrock_BaseURL(t *testing.T) {
	cfg := &Config{BaseURL: "https://bedrock-runtime.us-east-1.amazonaws.com"}
	assert.Equal(t, ProviderBedrock, DetectProvider(cfg))
}

func TestDetectProvider_Bedrock_OptOut(t *testing.T) {
	cfg := &Config{
		AWSAccessKeyID:     "key",
		AWSSecretAccessKey: "secret",
		BedrockDisabled:    true,
	}
	assert.Equal(t, ProviderAnthropic, DetectProvider(cfg))
}

func TestConfig_BedrockUseConverse_EnvVar(t *testing.T) {
	t.Setenv("REVENIUM_BEDROCK_USE_CONVERSE", "true")
	t.Setenv("REVENIUM_METERING_API_KEY", "test-key")
	cfg := &Config{}
	_ = cfg.loadFromEnv()
	assert.True(t, cfg.BedrockUseConverse)
}

func TestConfig_BedrockUseConverse_Default(t *testing.T) {
	cfg := &Config{}
	assert.False(t, cfg.BedrockUseConverse)
}

func TestConvertConverseStopReason(t *testing.T) {
	assert.Equal(t, "end_turn", convertConverseStopReason(brtypes.StopReasonEndTurn))
	assert.Equal(t, "max_tokens", convertConverseStopReason(brtypes.StopReasonMaxTokens))
	assert.Equal(t, "stop_sequence", convertConverseStopReason(brtypes.StopReasonStopSequence))
	assert.Equal(t, "tool_use", convertConverseStopReason(brtypes.StopReasonToolUse))
	assert.Equal(t, "content_filtered", convertConverseStopReason(brtypes.StopReasonContentFiltered))
	assert.Equal(t, "guardrail_intervened", convertConverseStopReason(brtypes.StopReasonGuardrailIntervened))
	assert.Equal(t, "future_reason", convertConverseStopReason("future_reason"))
}

func makeTestParams(model, text string) anthropicsdk.MessageNewParams {
	return anthropicsdk.MessageNewParams{
		Model:     anthropicsdk.Model(model),
		MaxTokens: 100,
		Messages: []anthropicsdk.MessageParam{
			{
				Role: "user",
				Content: []anthropicsdk.ContentBlockParamUnion{
					anthropicsdk.NewTextBlock(text),
				},
			},
		},
	}
}
