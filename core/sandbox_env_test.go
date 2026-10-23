package core

import (
	"os"
	"testing"

	"github.com/revenium/revenium-go-sdk/core/testutil/sandbox"
	"github.com/stretchr/testify/assert"
)

func TestSandboxPointsTheConfiguredReveniumAddressAtALocalPort(t *testing.T) {
	assert.Equal(t, sandbox.UnreachableAddress, LoadReveniumConfig().BaseURL)
}

func TestSandboxClearsEveryReveniumAccountSettingTheSDKReads(t *testing.T) {
	for _, name := range []string{EnvAPIKey, EnvEnforcementAPIKey, EnvEnforcementBaseURL, EnvTeamID} {
		_, set := os.LookupEnv(name)
		assert.False(t, set, name)
	}
}
