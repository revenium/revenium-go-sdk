package core

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadReveniumConfig_Defaults(t *testing.T) {
	clearReveniumEnv(t)

	cfg := LoadReveniumConfig()

	assert.Equal(t, "", cfg.APIKey)
	assert.Equal(t, DefaultBaseURL, cfg.BaseURL)
	assert.Equal(t, "", cfg.OrgID)
	assert.Equal(t, "", cfg.ProductID)
	assert.Equal(t, DefaultLogLevel, cfg.LogLevel)
	assert.False(t, cfg.Debug)
	assert.False(t, cfg.VerboseStartup)
}

func TestLoadReveniumConfig_FromEnv(t *testing.T) {
	clearReveniumEnv(t)

	t.Setenv(EnvAPIKey, "hak_test123")
	t.Setenv(EnvBaseURL, "https://api.dev.hcapp.io")
	t.Setenv(EnvOrgID, "org-123")
	t.Setenv(EnvProductID, "prod-456")
	t.Setenv(EnvLogLevel, "DEBUG")
	t.Setenv(EnvDebug, "true")
	t.Setenv(EnvVerboseStartup, "1")

	cfg := LoadReveniumConfig()

	assert.Equal(t, "hak_test123", cfg.APIKey)
	assert.Equal(t, "https://api.dev.hcapp.io", cfg.BaseURL)
	assert.Equal(t, "org-123", cfg.OrgID)
	assert.Equal(t, "prod-456", cfg.ProductID)
	assert.Equal(t, "DEBUG", cfg.LogLevel)
	assert.True(t, cfg.Debug)
	assert.True(t, cfg.VerboseStartup)
}

func TestLoadReveniumConfig_NormalizesBaseURL(t *testing.T) {
	clearReveniumEnv(t)
	t.Setenv(EnvBaseURL, "https://api.revenium.ai/meter/v2")

	cfg := LoadReveniumConfig()

	assert.Equal(t, "https://api.revenium.ai", cfg.BaseURL)
}

func TestValidateReveniumConfig_Valid(t *testing.T) {
	cfg := &ReveniumConfig{APIKey: "hak_valid_key"}
	assert.NoError(t, ValidateReveniumConfig(cfg))
}

func TestValidateReveniumConfig_MissingKey(t *testing.T) {
	cfg := &ReveniumConfig{}
	err := ValidateReveniumConfig(cfg)
	require.Error(t, err)
	assert.True(t, IsConfigError(err))
}

func TestValidateReveniumConfig_InvalidFormat(t *testing.T) {
	cfg := &ReveniumConfig{APIKey: "sk_invalid"}
	err := ValidateReveniumConfig(cfg)
	require.Error(t, err)
	assert.True(t, IsConfigError(err))
}

func TestMergeReveniumConfig_NilProgrammatic(t *testing.T) {
	env := &ReveniumConfig{APIKey: "hak_env", BaseURL: "https://env.example.com"}
	result := MergeReveniumConfig(nil, env)
	assert.Equal(t, env, result)
}

func TestMergeReveniumConfig_ProgrammaticTakesPrecedence(t *testing.T) {
	programmatic := &ReveniumConfig{
		APIKey:  "hak_programmatic",
		BaseURL: "https://programmatic.example.com",
		OrgID:   "org-prog",
	}
	env := &ReveniumConfig{
		APIKey:    "hak_env",
		BaseURL:   "https://env.example.com",
		OrgID:     "org-env",
		ProductID: "prod-env",
		LogLevel:  "DEBUG",
		Debug:     true,
	}

	result := MergeReveniumConfig(programmatic, env)

	assert.Equal(t, "hak_programmatic", result.APIKey)
	assert.Equal(t, "https://programmatic.example.com", result.BaseURL)
	assert.Equal(t, "org-prog", result.OrgID)
	assert.Equal(t, "prod-env", result.ProductID)
	assert.Equal(t, "DEBUG", result.LogLevel)
	assert.True(t, result.Debug)
}

func TestMergeReveniumConfig_FillsEmptyFields(t *testing.T) {
	programmatic := &ReveniumConfig{APIKey: "hak_mine"}
	env := &ReveniumConfig{
		APIKey:         "hak_env",
		BaseURL:        DefaultBaseURL,
		LogLevel:       DefaultLogLevel,
		VerboseStartup: true,
	}

	result := MergeReveniumConfig(programmatic, env)

	assert.Equal(t, "hak_mine", result.APIKey)
	assert.Equal(t, DefaultBaseURL, result.BaseURL)
	assert.Equal(t, DefaultLogLevel, result.LogLevel)
	assert.True(t, result.VerboseStartup)
}

func TestValidateReveniumConfig_Nil(t *testing.T) {
	err := ValidateReveniumConfig(nil)
	require.Error(t, err)
	assert.True(t, IsConfigError(err))
}

func TestValidateReveniumConfig_RejectsBaseURLWithoutScheme(t *testing.T) {
	err := ValidateReveniumConfig(&ReveniumConfig{APIKey: "hak_valid_key", BaseURL: "api.revenium.ai"})

	require.Error(t, err)
	assert.True(t, IsConfigError(err))
	assert.Contains(t, err.Error(), EnvBaseURL)
	assert.Contains(t, err.Error(), "https://")
}

func TestValidateReveniumConfig_RejectsEnforcementBaseURLWithoutScheme(t *testing.T) {
	err := ValidateReveniumConfig(&ReveniumConfig{
		APIKey:             "hak_valid_key",
		BaseURL:            DefaultBaseURL,
		EnforcementBaseURL: "rules.revenium.ai",
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), EnvEnforcementBaseURL)
}

func TestValidateReveniumConfig_AcceptsHTTPAndHTTPSBaseURLs(t *testing.T) {
	for _, baseURL := range []string{"https://api.revenium.ai", "http://localhost:8080", ""} {
		assert.NoError(t, ValidateReveniumConfig(&ReveniumConfig{APIKey: "hak_valid_key", BaseURL: baseURL}), baseURL)
	}
}

func TestValidateReveniumConfig_RejectsKeyWithInnerWhitespaceWithoutEchoingIt(t *testing.T) {
	for _, key := range []string{"hak_test\nkey", "hak_test key", "hak_test\tkey", "hak_test_key\r\n"} {
		err := ValidateReveniumConfig(&ReveniumConfig{APIKey: key, BaseURL: DefaultBaseURL})

		require.Error(t, err, "%q", key)
		assert.True(t, IsConfigError(err))
		assert.Contains(t, err.Error(), EnvAPIKey)
		assert.NotContains(t, err.Error(), "hak_test")
	}
}

func TestLoadReveniumConfig_TrimsPastedWhitespace(t *testing.T) {
	clearReveniumEnv(t)
	t.Setenv(EnvAPIKey, "hak_test_key\n")
	t.Setenv(EnvBaseURL, " https://api.dev.hcapp.io/\n")

	cfg := LoadReveniumConfig()

	assert.Equal(t, "hak_test_key", cfg.APIKey)
	assert.Equal(t, "https://api.dev.hcapp.io", cfg.BaseURL)
	assert.NoError(t, ValidateReveniumConfig(cfg))
}

func TestMergeReveniumConfig_TrimsProgrammaticKeyAndBaseURL(t *testing.T) {
	programmatic := &ReveniumConfig{APIKey: "hak_test_key\n", BaseURL: "https://api.dev.hcapp.io\r\n"}

	result := MergeReveniumConfig(programmatic, &ReveniumConfig{})

	assert.Equal(t, "hak_test_key", result.APIKey)
	assert.Equal(t, "https://api.dev.hcapp.io", result.BaseURL)
	assert.NoError(t, ValidateReveniumConfig(result))
}

func clearReveniumEnv(t *testing.T) {
	t.Helper()
	envVars := []string{EnvAPIKey, EnvEnforcementAPIKey, EnvBaseURL, EnvOrgID, EnvProductID, EnvLogLevel, EnvDebug, EnvVerboseStartup, EnvBufferMaxBytes}
	for _, env := range envVars {
		t.Setenv(env, "")
		os.Unsetenv(env)
	}
}

func TestLoadReveniumConfig_ReadsTheEnforcementKeySeparatelyFromTheMeteringKey(t *testing.T) {
	clearReveniumEnv(t)
	t.Setenv(EnvAPIKey, "rev_mk_metering")
	t.Setenv(EnvEnforcementAPIKey, "rev_rk_read")

	cfg := LoadReveniumConfig()

	assert.Equal(t, "rev_mk_metering", cfg.APIKey)
	assert.Equal(t, "rev_rk_read", cfg.EnforcementAPIKey)
}

func TestLoadReveniumConfig_LeavesTheEnforcementKeyEmptyWhenUnset(t *testing.T) {
	clearReveniumEnv(t)
	t.Setenv(EnvAPIKey, "rev_mk_metering")

	assert.Empty(t, LoadReveniumConfig().EnforcementAPIKey)
}

func TestMergeReveniumConfig_EnforcementKey(t *testing.T) {
	env := &ReveniumConfig{APIKey: "rev_mk_env", EnforcementAPIKey: "rev_rk_env"}

	assert.Equal(t, "rev_rk_env", MergeReveniumConfig(&ReveniumConfig{APIKey: "rev_mk_mine"}, env).EnforcementAPIKey)
	assert.Equal(t, "rev_rk_mine", MergeReveniumConfig(&ReveniumConfig{EnforcementAPIKey: "rev_rk_mine"}, env).EnforcementAPIKey)
}

func TestValidateReveniumBaseURL_NeverEchoesTheRejectedValue(t *testing.T) {
	err := ValidateReveniumConfig(&ReveniumConfig{APIKey: "hak_valid_key", BaseURL: "hak_swapped_secret_key"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), EnvBaseURL)
	assert.NotContains(t, err.Error(), "hak_swapped_secret_key")
}

func TestCheckReinitialization(t *testing.T) {
	cases := []struct {
		name          string
		firstEnv      map[string]string
		firstOptions  *ReveniumConfig
		repeatEnv     map[string]string
		repeatOptions *ReveniumConfig
		wantChanged   []string
	}{
		{name: "bare repeat after an options-only setup", firstOptions: &ReveniumConfig{APIKey: "hak_customer_a", BaseURL: "https://api.dev.hcapp.io", TeamID: "team-a"}},
		{name: "bare repeat when the environment disagrees with the first call's options", firstEnv: map[string]string{EnvAPIKey: "hak_env", EnvTeamID: "team-env"}, firstOptions: &ReveniumConfig{APIKey: "hak_vault", TeamID: "team-a"}},
		{name: "bare repeat after an environment-only setup", firstEnv: map[string]string{EnvAPIKey: "hak_customer_a", EnvBaseURL: "https://api.dev.hcapp.io"}},
		{name: "bare repeat after the environment changed under an environment setup", firstEnv: map[string]string{EnvAPIKey: "hak_customer_a"}, repeatEnv: map[string]string{EnvAPIKey: "hak_customer_b"}, wantChanged: []string{EnvAPIKey}},
		{name: "repeat naming another key", firstOptions: &ReveniumConfig{APIKey: "hak_customer_a"}, repeatOptions: &ReveniumConfig{APIKey: "hak_customer_b"}, wantChanged: []string{EnvAPIKey}},
		{name: "repeat naming the same base URL spelled differently", firstOptions: &ReveniumConfig{APIKey: "hak_customer_a", BaseURL: "https://api.dev.hcapp.io"}, repeatOptions: &ReveniumConfig{BaseURL: "https://api.dev.hcapp.io/meter/v2"}},
		{name: "padded team id on both sides", firstEnv: map[string]string{EnvAPIKey: "hak_customer_a", EnvTeamID: "team-a\n"}, repeatOptions: &ReveniumConfig{TeamID: " team-a "}},
		{name: "repeat naming a base URL when the first setup used the default", firstOptions: &ReveniumConfig{APIKey: "hak_customer_a"}, repeatOptions: &ReveniumConfig{BaseURL: "https://api.dev.hcapp.io"}, wantChanged: []string{EnvBaseURL}},
		{name: "repeat naming another account entirely", firstOptions: &ReveniumConfig{APIKey: "hak_customer_a", TeamID: "team-a"}, repeatOptions: &ReveniumConfig{APIKey: "hak_customer_b", BaseURL: "https://api.dev.hcapp.io", TeamID: "team-b"}, wantChanged: []string{EnvAPIKey, EnvBaseURL, EnvTeamID}},
		{name: "repeat naming another rules key", firstOptions: &ReveniumConfig{APIKey: "hak_customer_a", EnforcementAPIKey: "rev_rk_customer_a"}, repeatOptions: &ReveniumConfig{EnforcementAPIKey: "rev_rk_customer_b"}, wantChanged: []string{EnvEnforcementAPIKey}},
		{name: "repeat naming the same rules key", firstOptions: &ReveniumConfig{APIKey: "hak_customer_a", EnforcementAPIKey: "rev_rk_customer_a"}, repeatOptions: &ReveniumConfig{EnforcementAPIKey: " rev_rk_customer_a "}},
		{name: "repeat naming the metering key as the rules key when the first setup read rules with it", firstOptions: &ReveniumConfig{APIKey: "hak_customer_a"}, repeatOptions: &ReveniumConfig{EnforcementAPIKey: "hak_customer_a"}},
		{name: "repeat naming a rules key when the first setup read rules with the metering key", firstOptions: &ReveniumConfig{APIKey: "hak_customer_a"}, repeatOptions: &ReveniumConfig{EnforcementAPIKey: "rev_rk_customer_a"}, wantChanged: []string{EnvEnforcementAPIKey}},
		{name: "bare repeat after the rules key changed under an environment setup", firstEnv: map[string]string{EnvAPIKey: "hak_customer_a", EnvEnforcementAPIKey: "rev_rk_customer_a"}, repeatEnv: map[string]string{EnvEnforcementAPIKey: "rev_rk_customer_b"}, wantChanged: []string{EnvEnforcementAPIKey}},
		{name: "repeat naming another rules address", firstOptions: &ReveniumConfig{APIKey: "hak_customer_a", EnforcementBaseURL: "https://rules-a.example"}, repeatOptions: &ReveniumConfig{EnforcementBaseURL: "https://rules-b.example"}, wantChanged: []string{EnvEnforcementBaseURL}},
		{name: "repeat naming the metering address as the rules address when the first setup read rules there", firstOptions: &ReveniumConfig{APIKey: "hak_customer_a", BaseURL: "https://api.dev.hcapp.io"}, repeatOptions: &ReveniumConfig{EnforcementBaseURL: "https://api.dev.hcapp.io/"}},
		{name: "repeat naming the rules address without the /profitstream the first setup carried", firstOptions: &ReveniumConfig{APIKey: "hak_customer_a", EnforcementBaseURL: "https://rules-a.example/profitstream/"}, repeatOptions: &ReveniumConfig{EnforcementBaseURL: "https://rules-a.example"}},
		{name: "bare repeat after the rules address changed under an environment setup", firstEnv: map[string]string{EnvAPIKey: "hak_customer_a", EnvEnforcementBaseURL: "https://rules-a.example"}, repeatEnv: map[string]string{EnvEnforcementBaseURL: "https://rules-b.example"}, wantChanged: []string{EnvEnforcementBaseURL}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			clearReveniumEnv(t)
			t.Setenv(EnvTeamID, "")
			t.Setenv(EnvEnforcementBaseURL, "")
			for name, value := range tc.firstEnv {
				t.Setenv(name, value)
			}
			options := tc.firstOptions
			if options == nil {
				options = &ReveniumConfig{}
			}
			active := MergeReveniumConfig(options, LoadReveniumConfig())
			for name, value := range tc.repeatEnv {
				t.Setenv(name, value)
			}

			err := CheckReinitialization(active, tc.repeatOptions, "NewReveniumOpenAI")

			if len(tc.wantChanged) == 0 {
				assert.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.True(t, IsConfigError(err))
			for _, setting := range tc.wantChanged {
				assert.Contains(t, err.Error(), setting)
			}
			assert.Contains(t, err.Error(), "NewReveniumOpenAI")
			assert.NotContains(t, err.Error(), "hak_customer")
			assert.NotContains(t, err.Error(), "rev_rk_customer")
		})
	}
}

func TestNamedAccount_CopiesOnlyTheAccountSettingsBeforeTheEnvironmentIsMerged(t *testing.T) {
	clearReveniumEnv(t)
	t.Setenv(EnvAPIKey, "hak_from_env")
	programmatic := &ReveniumConfig{TeamID: " team-a ", OrgID: "org"}

	named := NamedAccount(programmatic)
	MergeReveniumConfig(programmatic, LoadReveniumConfig())

	assert.Nil(t, NamedAccount(nil))
	assert.Equal(t, &ReveniumConfig{TeamID: "team-a"}, named)
	assert.Equal(t, "hak_from_env", programmatic.APIKey, "merging changes the caller's config, not the copy")
}
func TestTrimReveniumConfig_TrimsEveryAccountSettingAndToleratesNil(t *testing.T) {
	cfg := &ReveniumConfig{
		APIKey:             "hak_key\n",
		BaseURL:            " https://api.dev.hcapp.io ",
		EnforcementBaseURL: "https://rules.example\r\n",
		EnforcementAPIKey:  "\trev_rk\n",
		TeamID:             " team-a\n",
	}

	TrimReveniumConfig(cfg)
	TrimReveniumConfig(nil)

	assert.Equal(t, "hak_key", cfg.APIKey)
	assert.Equal(t, "https://api.dev.hcapp.io", cfg.BaseURL)
	assert.Equal(t, "https://rules.example", cfg.EnforcementBaseURL)
	assert.Equal(t, "rev_rk", cfg.EnforcementAPIKey)
	assert.Equal(t, "team-a", cfg.TeamID)
}

func TestLoadReveniumConfig_ReadsTheBufferByteLimit(t *testing.T) {
	cases := map[string]int64{"": 0, "2000000": 2000000, " 2000000 ": 2000000, "0": 0, "-5": 0, "10MB": 0}
	for raw, want := range cases {
		t.Run(raw, func(t *testing.T) {
			clearReveniumEnv(t)
			t.Setenv(EnvBufferMaxBytes, raw)
			assert.Equal(t, want, LoadReveniumConfig().BufferMaxBytes)
		})
	}
}

func TestMergeReveniumConfig_BufferByteLimitSetInCodeBeatsTheEnvironment(t *testing.T) {
	clearReveniumEnv(t)
	t.Setenv(EnvBufferMaxBytes, "2000000")

	assert.Equal(t, int64(500000), MergeReveniumConfig(&ReveniumConfig{BufferMaxBytes: 500000}, LoadReveniumConfig()).BufferMaxBytes)
	assert.Equal(t, int64(2000000), MergeReveniumConfig(&ReveniumConfig{}, LoadReveniumConfig()).BufferMaxBytes)
}

func TestNamedAccount_CopiesTheCostControlSettings(t *testing.T) {
	named := NamedAccount(&ReveniumConfig{EnforcementAPIKey: " rev_rk_read\n", EnforcementBaseURL: " https://rules.example "})

	assert.Equal(t, &ReveniumConfig{EnforcementAPIKey: "rev_rk_read", EnforcementBaseURL: "https://rules.example"}, named)
}
