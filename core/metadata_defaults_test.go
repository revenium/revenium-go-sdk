package core

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func clearMetadataDefaultEnv(t *testing.T) {
	t.Helper()
	for _, name := range []string{EnvOrganizationName, EnvOrgID, EnvProductName, EnvProductID} {
		unsetForTest(t, name)
	}
	for _, source := range metadataDefaultSources {
		for _, name := range source.variables {
			unsetForTest(t, name)
		}
	}
	unsetForTest(t, EnvRetryNumber)
}

func TestLoadMetadataDefaults_ReadsEachDocumentedVariable(t *testing.T) {
	clearMetadataDefaultEnv(t)
	t.Setenv(EnvOrganizationName, "acme")
	t.Setenv(EnvProductName, "checkout")
	t.Setenv(EnvEnvironment, "production")
	t.Setenv(EnvRegion, "us-east-1")
	t.Setenv(EnvTraceType, "checkout-flow")
	t.Setenv(EnvTraceName, "Checkout")
	t.Setenv(EnvParentTransactionID, "parent-1")
	t.Setenv(EnvTransactionName, "refund")
	t.Setenv(EnvCredentialAlias, "prod-key")
	t.Setenv(EnvRetryNumber, "2")

	assert.Equal(t, map[string]interface{}{
		"environment":         "production",
		"region":              "us-east-1",
		"traceType":           "checkout-flow",
		"traceName":           "Checkout",
		"parentTransactionId": "parent-1",
		"transactionName":     "refund",
		"credentialAlias":     "prod-key",
		"retryNumber":         2,
	}, LoadMetadataDefaults())
}

func TestLoadMetadataDefaults_FallsBackInOrder(t *testing.T) {
	clearMetadataDefaultEnv(t)
	t.Setenv(EnvOrgID, "org-from-id")
	t.Setenv(EnvProductID, "product-from-id")
	t.Setenv("DEPLOYMENT_ENV", "staging")
	t.Setenv("GOOGLE_CLOUD_REGION", "europe-west1")
	t.Setenv("AWS_DEFAULT_REGION", "us-west-2")

	defaults := LoadMetadataDefaults()
	account := LoadReveniumConfig()

	assert.Equal(t, "org-from-id", account.OrgID)
	assert.Equal(t, "product-from-id", account.ProductID)
	assert.NotContains(t, defaults, "organizationName", "organization and product are read only by LoadReveniumConfig")
	assert.Equal(t, "staging", defaults["environment"])
	assert.Equal(t, "us-west-2", defaults["region"])
}

func TestLoadMetadataDefaults_SkipsBlankAndInvalidValues(t *testing.T) {
	clearMetadataDefaultEnv(t)
	t.Setenv(EnvEnvironment, "  ")
	t.Setenv(EnvRetryNumber, "second")

	assert.Empty(t, LoadMetadataDefaults())
}

func TestReadReveniumEnv_OrganizationNameWinsOverOrganizationID(t *testing.T) {
	clearMetadataDefaultEnv(t)
	t.Setenv(EnvOrganizationName, "by-name")
	t.Setenv(EnvOrgID, "by-id")

	assert.Equal(t, "by-name", LoadReveniumConfig().OrgID)
}

func TestMeteringTimeout(t *testing.T) {
	cases := map[string]time.Duration{
		"":       DefaultMeteringTimeout,
		"5s":     5 * time.Second,
		"1500ms": 1500 * time.Millisecond,
		"2":      2 * time.Second,
		"0.5":    500 * time.Millisecond,
		"later":  DefaultMeteringTimeout,
		"-1s":    DefaultMeteringTimeout,
		"0":      DefaultMeteringTimeout,
		"NaN":    DefaultMeteringTimeout,
		"Inf":    DefaultMeteringTimeout,
		"-Inf":   DefaultMeteringTimeout,
		"1e-12":  DefaultMeteringTimeout,
		"1e300":  DefaultMeteringTimeout,
		"0s":     DefaultMeteringTimeout,
	}
	for value, want := range cases {
		t.Run(value, func(t *testing.T) {
			t.Setenv(EnvAPITimeout, value)

			assert.Equal(t, want, MeteringTimeout())
		})
	}
}

func TestIsEnvFlagSet(t *testing.T) {
	cases := map[string]bool{"true": true, "TRUE": true, " true ": true, "1": true, "": false, "false": false, "yes": false, "0": false}
	for value, want := range cases {
		t.Run(value, func(t *testing.T) {
			t.Setenv("REVENIUM_TEST_FLAG", value)

			assert.Equal(t, want, IsEnvFlagSet("REVENIUM_TEST_FLAG"))
		})
	}
}

func TestLoadReveniumConfig_ParsesTheDebugFlagLikeEveryOtherFlag(t *testing.T) {
	for _, value := range []string{"TRUE", " true ", "1"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv(EnvDebug, value)
			t.Setenv(EnvVerboseStartup, value)

			cfg := LoadReveniumConfig()

			assert.True(t, cfg.Debug)
			assert.True(t, cfg.VerboseStartup)
		})
	}
}

func TestInitializeLogger_LogLevelStillAppliesWithoutTheDebugFlag(t *testing.T) {
	original := globalLogger.GetLevel()
	t.Cleanup(func() { globalLogger.SetLevel(original) })
	require.NoError(t, os.Unsetenv(EnvDebug))
	t.Setenv(EnvDebug, "")
	t.Setenv(EnvLogLevel, "warn")

	InitializeLogger()

	assert.Equal(t, LogLevelWarn, globalLogger.GetLevel())
}

func TestMergeReveniumConfig_AppliesTheDebugSettingToTheLogger(t *testing.T) {
	original := globalLogger.GetLevel()
	t.Cleanup(func() {
		SetGlobalDebug(false)
		globalLogger.SetLevel(original)
	})
	globalLogger.SetLevel(LogLevelInfo)
	t.Setenv(EnvLogLevel, "")
	t.Setenv(EnvDebug, "true")

	MergeReveniumConfig(&ReveniumConfig{}, LoadReveniumConfig())
	InitializeLogger()

	assert.Equal(t, LogLevelDebug, globalLogger.GetLevel(), "a later InitializeLogger keeps the debug the config asked for")
}
