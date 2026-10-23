package enforcement

import (
	"testing"

	"github.com/revenium/revenium-go-sdk/core"
	"github.com/stretchr/testify/assert"
)

func TestStartForUsesEachClientsOwnKeyOverAProcessWideEnforcementKey(t *testing.T) {
	rules := newKeyRecordingRulesServer(t, "")
	t.Setenv(core.EnvEnforcementAPIKey, "rev_rk_process_wide")
	t.Setenv(core.EnvEnforcementBaseURL, "")

	resetEngines()
	defer Stop()
	StartFor(&core.ReveniumConfig{APIKey: "hak_account_a", BaseURL: rules.URL, TeamID: "team-a"})
	StartFor(&core.ReveniumConfig{APIKey: "hak_account_b", BaseURL: rules.URL, TeamID: "team-b"})

	assert.ElementsMatch(t, []string{
		core.EnforcementRulesPathPrefix + "team-a hak_account_a",
		core.EnforcementRulesPathPrefix + "team-b hak_account_b",
	}, rules.requests(), "each account fetches its rules with its own key")
}

func TestStartForPrefersTheConfigsEnforcementSettingsOverEverythingElse(t *testing.T) {
	rules := newKeyRecordingRulesServer(t, "rev_rk_config")
	fromEnv := newKeyRecordingRulesServer(t, "rev_rk_env")
	t.Setenv(core.EnvEnforcementBaseURL, fromEnv.URL)
	t.Setenv(core.EnvEnforcementAPIKey, "rev_rk_env")

	resetEngines()
	defer Stop()
	StartFor(&core.ReveniumConfig{APIKey: "rev_mk_metering", BaseURL: fromEnv.URL, EnforcementBaseURL: rules.URL, EnforcementAPIKey: "rev_rk_config", TeamID: testTeamID})

	assert.Equal(t, []string{rulesPath + " rev_rk_config"}, rules.requests())
	assert.Empty(t, fromEnv.requests())
}

func TestStartForUsesTheEnforcementAddressFromTheEnvironmentOnlyWhenTheClientSetsNoAddress(t *testing.T) {
	rules := newKeyRecordingRulesServer(t, "rev_mk_metering")
	t.Setenv(core.EnvBaseURL, "http://127.0.0.1:9")
	t.Setenv(core.EnvEnforcementBaseURL, rules.URL)
	t.Setenv(core.EnvEnforcementAPIKey, "rev_rk_process_wide")

	resetEngines()
	defer Stop()
	StartFor(&core.ReveniumConfig{APIKey: "rev_mk_metering", TeamID: testTeamID})

	assert.Equal(t, []string{rulesPath + " rev_mk_metering"}, rules.requests(), "the client's own key wins over the process-wide one")
}

func TestStartForFallsBackToTheMeteringAddressWhenNoAddressIsSetAnywhere(t *testing.T) {
	rules := newKeyRecordingRulesServer(t, "rev_mk_metering")
	t.Setenv(core.EnvEnforcementBaseURL, "")
	t.Setenv(core.EnvEnforcementAPIKey, "")
	t.Setenv(core.EnvBaseURL, rules.URL)

	resetEngines()
	defer Stop()
	StartFor(&core.ReveniumConfig{APIKey: "rev_mk_metering", TeamID: testTeamID})

	assert.Equal(t, []string{rulesPath + " rev_mk_metering"}, rules.requests())
}

func TestStartForWithAnInitializeMergedConfigStillUsesTheEnvironmentsEnforcementSettings(t *testing.T) {
	rules := newKeyRecordingRulesServer(t, "rev_rk_read")
	t.Setenv(core.EnvAPIKey, "rev_mk_metering")
	t.Setenv(core.EnvBaseURL, "http://127.0.0.1:9")
	t.Setenv(core.EnvEnforcementBaseURL, rules.URL)
	t.Setenv(core.EnvEnforcementAPIKey, "rev_rk_read")
	t.Setenv(core.EnvTeamID, testTeamID)

	resetEngines()
	defer Stop()
	StartFor(core.MergeReveniumConfig(&core.ReveniumConfig{}, core.LoadReveniumConfig()))

	assert.Equal(t, []string{rulesPath + " rev_rk_read"}, rules.requests())
}
