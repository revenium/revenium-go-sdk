package openai

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/revenium/revenium-go-sdk/core/enforcement"
	"github.com/revenium/revenium-go-sdk/core/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func organizationRule(organization string) string {
	return `{"rules": [{"ruleId": 11, "threshold": 10, "currentValue": 12, "breached": true,
		"shadowMode": false, "action": "BLOCK",
		"filters": [{"dimension": "ORGANIZATION", "operator": "IS", "value": "` + organization + `"}]}]}`
}

func TestStandaloneClient_TheEnvironmentOrganizationReachesTheRecordAndTheCostControlCheck(t *testing.T) {
	unsetEnv(t, core.EnvOrgID, core.EnvProductName, core.EnvProductID)
	t.Setenv(core.EnvOrganizationName, "env-org")

	t.Run("the record carries it", func(t *testing.T) {
		enforcement.Stop()
		t.Cleanup(enforcement.Stop)
		provider := newProviderStub(t)
		rules := testutil.NewMockRulesServer()
		t.Cleanup(rules.Close)
		rules.SetPayload(enforcementTestTeam, organizationRule("other-org"))
		metering := testutil.NewMockMeteringServer()
		t.Cleanup(metering.Close)
		client := newEnforcedOpenAI(t, provider.server.URL, rules.URL(), metering.URL(), enforcementTestTeam)

		_, err := client.Chat().Completions().New(context.Background(), chatParams("gpt-4.1-nano"))
		require.NoError(t, err)
		client.Flush()

		require.True(t, metering.WaitForPayloads(1, 2*time.Second))
		assert.Equal(t, "env-org", metering.GetPayloads()[0]["organizationName"])
	})

	t.Run("a rule on it blocks the call", func(t *testing.T) {
		enforcement.Stop()
		t.Cleanup(enforcement.Stop)
		provider := newProviderStub(t)
		rules := testutil.NewMockRulesServer()
		t.Cleanup(rules.Close)
		rules.SetPayload(enforcementTestTeam, organizationRule("env-org"))
		metering := testutil.NewMockMeteringServer()
		t.Cleanup(metering.Close)
		client := newEnforcedOpenAI(t, provider.server.URL, rules.URL(), metering.URL(), enforcementTestTeam)

		_, err := client.Chat().Completions().New(context.Background(), chatParams("gpt-4.1-nano"))

		var ece *enforcement.ErrCostLimitExceeded
		require.True(t, errors.As(err, &ece), "a breached rule on the environment organization should block, got %v", err)
		assert.Equal(t, int32(0), provider.hits.Load())
	})
}
