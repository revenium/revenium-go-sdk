package openai

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	openaisdk "github.com/openai/openai-go/v3"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/revenium/revenium-go-sdk/core/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChatRecordCarriesTheEnvironmentDefaults(t *testing.T) {
	unsetEnv(t, core.EnvOrgID, core.EnvProductName, core.EnvProductID)
	t.Setenv(core.EnvEnvironment, "production")
	t.Setenv(core.EnvTraceType, "checkout")
	t.Setenv(core.EnvOrganizationName, "acme")

	payload := meteredChatPayload(t, context.Background())

	assert.Equal(t, "production", payload["environment"])
	assert.Equal(t, "checkout", payload["traceType"])
	assert.Equal(t, "acme", payload["organizationName"])
}

func TestChatRecordPrefersTheCallsOwnValuesOverEnvironmentDefaults(t *testing.T) {
	t.Setenv(core.EnvEnvironment, "production")
	t.Setenv(core.EnvTraceType, "checkout")
	ctx := core.WithUsageMetadata(context.Background(), map[string]interface{}{"environment": "staging", "traceType": "refund"})

	payload := meteredChatPayload(t, ctx)

	assert.Equal(t, "staging", payload["environment"])
	assert.Equal(t, "refund", payload["traceType"])
}

func TestChatRecordPrefersTheOrganizationAndProductSetInCodeOverTheEnvironment(t *testing.T) {
	t.Setenv(core.EnvOrganizationName, "env-org")
	t.Setenv(core.EnvProductName, "env-product")
	openaiServer := httptest.NewServer(chatCompletionHandler())
	defer openaiServer.Close()
	mock := testutil.NewMockMeteringServer()
	defer mock.Close()
	r, err := NewReveniumOpenAI(&Config{
		OpenAIAPIKey: "sk-test",
		BaseURL:      openaiServer.URL,
		Revenium:     &core.ReveniumConfig{APIKey: "hak_test_key_123", BaseURL: mock.URL(), OrgID: "code-org", ProductID: "code-product"},
	})
	require.NoError(t, err)
	defer r.Close()

	_, err = r.Chat().Completions().New(context.Background(), openaisdk.ChatCompletionNewParams{
		Model:    "gpt-4.1-nano",
		Messages: []openaisdk.ChatCompletionMessageParamUnion{openaisdk.UserMessage("hi")},
	})
	require.NoError(t, err)
	r.Flush()

	require.True(t, mock.WaitForPayloads(1, 2*time.Second))
	assert.Equal(t, "code-org", mock.GetPayloads()[0]["organizationName"])
	assert.Equal(t, "code-product", mock.GetPayloads()[0]["productName"])
}
