package enforcement

import (
	"context"
	"errors"
	"testing"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/revenium/revenium-go-sdk/core/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func engineWithBlockingRule(t *testing.T, dimension, value string, rc core.ReveniumConfig) *Engine {
	t.Helper()
	rules := testutil.NewMockRulesServer()
	t.Cleanup(rules.Close)
	rules.SetPayload("team-a", `{"rules": [{"ruleId": 7, "threshold": 10, "currentValue": 12, "breached": true,
		"shadowMode": false, "action": "BLOCK",
		"filters": [{"dimension": "`+dimension+`", "operator": "IS", "value": "`+value+`"}]}]}`)
	resetEngines()
	t.Cleanup(Stop)
	rc.APIKey, rc.BaseURL, rc.TeamID = "key-a", rules.URL(), "team-a"
	return StartFor(&rc)
}

func clearDefaultEnv(t *testing.T) {
	t.Helper()
	for _, name := range []string{core.EnvOrganizationName, core.EnvOrgID, core.EnvProductName, core.EnvProductID, core.EnvCredentialAlias} {
		t.Setenv(name, "")
	}
}

func isBlocked(err error) bool {
	var ece *ErrCostLimitExceeded
	return errors.As(err, &ece)
}

func TestCheck_ARuleOnTheDefaultOrganizationBlocksCallsThatNameNone(t *testing.T) {
	clearDefaultEnv(t)
	t.Setenv(core.EnvOrganizationName, "acme")
	engine := engineWithBlockingRule(t, "ORGANIZATION", "acme", core.ReveniumConfig{})

	assert.True(t, isBlocked(engine.Check(NewEvalContext(context.Background(), "gpt-4.1-nano", "OPENAI"))))
	other := core.WithUsageMetadata(context.Background(), map[string]interface{}{"organizationName": "globex"})
	assert.NoError(t, engine.Check(NewEvalContext(other, "gpt-4.1-nano", "OPENAI")), "the call's own organization wins")
}

func TestCheck_ARuleOnTheConfiguredProductBlocksCallsThatNameNone(t *testing.T) {
	clearDefaultEnv(t)
	engine := engineWithBlockingRule(t, "PRODUCT", "storefront", core.ReveniumConfig{ProductID: "storefront"})

	assert.True(t, isBlocked(engine.Check(NewEvalContext(context.Background(), "gpt-4.1-nano", "OPENAI"))))
}

func TestCheck_ARuleOnTheCredentialAliasBlocksCallsWithoutTheirOwnCredential(t *testing.T) {
	clearDefaultEnv(t)
	t.Setenv(core.EnvCredentialAlias, "prod-key")
	engine := engineWithBlockingRule(t, "CREDENTIAL", "prod-key", core.ReveniumConfig{})

	require.True(t, isBlocked(engine.Check(NewEvalContext(context.Background(), "gpt-4.1-nano", "OPENAI"))))
	person := core.WithSubscriber(context.Background(), &core.Subscriber{ID: "user-1"})
	assert.NoError(t, engine.Check(NewEvalContext(person, "gpt-4.1-nano", "OPENAI")), "the record carries no alias for a named subscriber, so neither does the check")
}

func TestNewEvalContext_UsesTheCallsCredentialAliasLikeItsRecord(t *testing.T) {
	ctx := core.WithUsageMetadata(context.Background(), map[string]interface{}{"credentialAlias": "team-key"})

	assert.Equal(t, "team-key", NewEvalContext(ctx, "gpt-4.1-nano", "OPENAI").Credential)
}

func TestCheck_APaddedCredentialAliasIsBlockedByARuleOnTheTrimmedName(t *testing.T) {
	clearDefaultEnv(t)
	engine := engineWithBlockingRule(t, "CREDENTIAL", "prod-key", core.ReveniumConfig{})
	ctx := core.WithUsageMetadata(context.Background(), map[string]interface{}{"credentialAlias": " prod-key "})

	assert.True(t, isBlocked(engine.Check(NewEvalContext(ctx, "gpt-4.1-nano", "OPENAI"))))
}

func TestNewEvalContext_AFilledCredentialValueWinsOverTheAliasWhenTheNameIsBlank(t *testing.T) {
	ctx := core.WithUsageMetadata(context.Background(), map[string]interface{}{
		"credentialAlias": "team-key",
		"subscriber":      map[string]interface{}{"credential": map[string]interface{}{"name": "  ", "value": "pk-123"}},
	})

	assert.Equal(t, "pk-123", NewEvalContext(ctx, "gpt-4.1-nano", "OPENAI").Credential)
}

func TestNewEvalContext_ReadsTheCredentialTheWayTheRecordDoes(t *testing.T) {
	cases := map[string]struct {
		credential interface{}
		want       string
	}{
		"string map with a name": {map[string]string{"name": "caller-key"}, "caller-key"},
		"string map without one": {map[string]string{"value": ""}, "team-key"},
		"unreadable credential":  {"caller-key", ""},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			ctx := core.WithUsageMetadata(context.Background(), map[string]interface{}{
				"credentialAlias": "team-key",
				"subscriber":      map[string]interface{}{"credential": tc.credential},
			})

			assert.Equal(t, tc.want, NewEvalContext(ctx, "gpt-4.1-nano", "OPENAI").Credential)
		})
	}
}
