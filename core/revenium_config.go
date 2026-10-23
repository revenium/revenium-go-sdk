package core

import (
	"fmt"
	"net/url"
	"os"
	"strings"
)

type ReveniumConfig struct {
	APIKey             string
	BaseURL            string
	EnforcementBaseURL string
	EnforcementAPIKey  string
	OrgID              string
	ProductID          string
	TeamID             string
	LogLevel           string
	Debug              bool
	VerboseStartup     bool
	// BufferMaxBytes bounds the memory a client's held usage records and its
	// records waiting for their first send take together; zero means
	// REVENIUM_BUFFER_MAX_BYTES, or DefaultBufferMaxBytes when that is unset.
	BufferMaxBytes int64

	fromEnv accountSettings
}

type accountSettings uint8

const (
	settingAPIKey accountSettings = 1 << iota
	settingBaseURL
	settingTeamID
	settingEnforcementAPIKey
	settingEnforcementBaseURL
)

func LoadReveniumConfig() *ReveniumConfig {
	cfg := readReveniumEnv()
	cfg.fromEnv = cfg.namedAccountSettings()
	if cfg.BaseURL == "" {
		cfg.BaseURL = DefaultBaseURL
	}
	cfg.BaseURL = NormalizeReveniumBaseURL(cfg.BaseURL)
	if cfg.EnforcementBaseURL != "" {
		cfg.EnforcementBaseURL = NormalizeReveniumBaseURL(cfg.EnforcementBaseURL)
	}
	if cfg.LogLevel == "" {
		cfg.LogLevel = DefaultLogLevel
	}
	return cfg
}

func readReveniumEnv() *ReveniumConfig {
	return &ReveniumConfig{
		APIKey:             strings.TrimSpace(os.Getenv(EnvAPIKey)),
		EnforcementAPIKey:  strings.TrimSpace(os.Getenv(EnvEnforcementAPIKey)),
		BaseURL:            strings.TrimSpace(os.Getenv(EnvBaseURL)),
		EnforcementBaseURL: strings.TrimSpace(os.Getenv(EnvEnforcementBaseURL)),
		OrgID:              firstEnvValue(EnvOrganizationName, EnvOrgID),
		ProductID:          firstEnvValue(EnvProductName, EnvProductID),
		TeamID:             strings.TrimSpace(os.Getenv(EnvTeamID)),
		LogLevel:           os.Getenv(EnvLogLevel),
		Debug:              IsEnvFlagSet(EnvDebug),
		VerboseStartup:     IsEnvFlagSet(EnvVerboseStartup),
		BufferMaxBytes:     envBufferMaxBytes(),
	}
}

func (c *ReveniumConfig) namedAccountSettings() accountSettings {
	var named accountSettings
	if c.APIKey != "" {
		named |= settingAPIKey
	}
	if c.BaseURL != "" {
		named |= settingBaseURL
	}
	if c.TeamID != "" {
		named |= settingTeamID
	}
	if c.EnforcementAPIKey != "" {
		named |= settingEnforcementAPIKey
	}
	if c.EnforcementBaseURL != "" {
		named |= settingEnforcementBaseURL
	}
	return named
}

// MergeReveniumConfig fills every setting programmatic leaves unset from env
// and turns debug logging on when the result asks for it, so REVENIUM_DEBUG
// and a debug option take effect in every module that loads its config this
// way, and a config without debug never turns off debug another client chose.
func MergeReveniumConfig(programmatic, env *ReveniumConfig) *ReveniumConfig {
	merged := mergeReveniumConfig(programmatic, env)
	EnableDebugWhenRequested(merged.Debug)
	return merged
}

// DebugRequested reports whether this config asks for debug logging, with
// REVENIUM_DEBUG standing in for a config built by hand rather than loaded
// with LoadReveniumConfig, so a standalone client honours it too.
func (c *ReveniumConfig) DebugRequested() bool {
	return c.Debug || IsEnvFlagSet(EnvDebug)
}

func mergeReveniumConfig(programmatic, env *ReveniumConfig) *ReveniumConfig {
	if programmatic == nil {
		return env
	}
	TrimReveniumConfig(programmatic)
	if programmatic.APIKey == "" {
		programmatic.APIKey = env.APIKey
		programmatic.fromEnv |= env.fromEnv & settingAPIKey
	}
	if programmatic.BaseURL == "" {
		programmatic.BaseURL = env.BaseURL
		programmatic.fromEnv |= env.fromEnv & settingBaseURL
	}
	if programmatic.EnforcementBaseURL == "" {
		programmatic.EnforcementBaseURL = env.EnforcementBaseURL
		programmatic.fromEnv |= env.fromEnv & settingEnforcementBaseURL
	}
	if programmatic.EnforcementAPIKey == "" {
		programmatic.EnforcementAPIKey = env.EnforcementAPIKey
		programmatic.fromEnv |= env.fromEnv & settingEnforcementAPIKey
	}
	if programmatic.OrgID == "" {
		programmatic.OrgID = env.OrgID
	}
	if programmatic.ProductID == "" {
		programmatic.ProductID = env.ProductID
	}
	if programmatic.TeamID == "" {
		programmatic.TeamID = env.TeamID
		programmatic.fromEnv |= env.fromEnv & settingTeamID
	}
	if programmatic.LogLevel == "" {
		programmatic.LogLevel = env.LogLevel
	}
	if !programmatic.Debug {
		programmatic.Debug = env.Debug
	}
	if !programmatic.VerboseStartup {
		programmatic.VerboseStartup = env.VerboseStartup
	}
	if programmatic.BufferMaxBytes <= 0 {
		programmatic.BufferMaxBytes = env.BufferMaxBytes
	}
	return programmatic
}

// TrimReveniumConfig removes leading and trailing whitespace, such as the
// newline a secret file leaves behind, from the Revenium keys, base URLs and
// team id. Initialize and every standalone constructor apply it, so both
// setup paths accept the same values.
func TrimReveniumConfig(cfg *ReveniumConfig) {
	if cfg == nil {
		return
	}
	cfg.APIKey = strings.TrimSpace(cfg.APIKey)
	cfg.BaseURL = strings.TrimSpace(cfg.BaseURL)
	cfg.EnforcementBaseURL = strings.TrimSpace(cfg.EnforcementBaseURL)
	cfg.EnforcementAPIKey = strings.TrimSpace(cfg.EnforcementAPIKey)
	cfg.TeamID = strings.TrimSpace(cfg.TeamID)
}

func ValidateReveniumConfig(cfg *ReveniumConfig) error {
	if cfg == nil {
		return NewConfigError("ReveniumConfig is nil", nil)
	}
	if cfg.APIKey == "" {
		return NewConfigError(EnvAPIKey+" is required", nil)
	}
	if err := ValidateReveniumAPIKeyCharacters(cfg.APIKey); err != nil {
		return err
	}
	if !IsValidAPIKeyFormat(cfg.APIKey) {
		return NewConfigError(EnvAPIKey+" must start with 'hak_' or 'rev_'", nil)
	}
	if err := validateKeyCharacters(EnvEnforcementAPIKey, cfg.EnforcementAPIKey); err != nil {
		return err
	}
	if err := ValidateReveniumBaseURL(EnvBaseURL, cfg.BaseURL); err != nil {
		return err
	}
	return ValidateReveniumBaseURL(EnvEnforcementBaseURL, cfg.EnforcementBaseURL)
}

// ValidateReveniumAPIKeyCharacters rejects a key that cannot travel in an
// HTTP header. The key itself is never echoed, because it is a secret.
func ValidateReveniumAPIKeyCharacters(apiKey string) error {
	return validateKeyCharacters(EnvAPIKey, apiKey)
}

func validateKeyCharacters(setting, key string) error {
	for _, r := range key {
		if r <= ' ' || r == 0x7f {
			return NewConfigError(setting+" contains a space, tab or line break; remove it so requests to Revenium can be sent", nil)
		}
	}
	return nil
}

// ValidateReveniumBaseURL accepts an empty value, which means "use the
// default", and otherwise requires an absolute http or https URL.
func ValidateReveniumBaseURL(setting, baseURL string) error {
	if baseURL == "" {
		return nil
	}
	u, err := url.Parse(baseURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return NewConfigError(setting+" must be an absolute URL starting with https:// or http://; the value is not shown because it may hold a credential", nil)
	}
	return nil
}

// NamedAccount copies the Revenium account settings a caller's options set,
// before environment values or defaults are merged into them, for the
// repeat-Initialize check.
func NamedAccount(programmatic *ReveniumConfig) *ReveniumConfig {
	if programmatic == nil {
		return nil
	}
	return &ReveniumConfig{
		APIKey:             strings.TrimSpace(programmatic.APIKey),
		BaseURL:            strings.TrimSpace(programmatic.BaseURL),
		TeamID:             strings.TrimSpace(programmatic.TeamID),
		EnforcementAPIKey:  strings.TrimSpace(programmatic.EnforcementAPIKey),
		EnforcementBaseURL: strings.TrimSpace(programmatic.EnforcementBaseURL),
	}
}

// CheckReinitialization returns nil when a repeat Initialize call asks for
// the account the module is already set up with, and a config error naming
// each setting that differs otherwise. A setting counts as asked for when the
// call's options name it, or when the first setup took it from the
// environment and the environment still sets it; anything else is unchanged.
// The first setup stays in force either way, so without this error a second
// account's usage would silently be recorded under the first one.
func CheckReinitialization(active, named *ReveniumConfig, standaloneConstructor string) error {
	if active == nil {
		active = &ReveniumConfig{}
	}
	requested := requestedAccount(active, named)
	var changed []string
	if requested.APIKey != "" && active.APIKey != requested.APIKey {
		changed = append(changed, EnvAPIKey)
	}
	if requested.BaseURL != "" && NormalizeReveniumBaseURL(active.BaseURL) != NormalizeReveniumBaseURL(requested.BaseURL) {
		changed = append(changed, EnvBaseURL)
	}
	if requested.TeamID != "" && active.TeamID != requested.TeamID {
		changed = append(changed, EnvTeamID)
	}
	if requested.EnforcementAPIKey != "" && active.RulesAPIKey() != requested.EnforcementAPIKey {
		changed = append(changed, EnvEnforcementAPIKey)
	}
	if requested.EnforcementBaseURL != "" && !sameRulesAddress(active.RulesBaseURL(), requested.EnforcementBaseURL) {
		changed = append(changed, EnvEnforcementBaseURL)
	}
	if len(changed) == 0 {
		return nil
	}
	return NewConfigError(fmt.Sprintf(
		"Initialize was already called with a different %s; this call was ignored, and usage is still recorded "+
			"and cost controls still read with the first setup. To meter several Revenium accounts from one process, "+
			"create one client per account with %s instead of Initialize",
		strings.Join(changed, ", "), standaloneConstructor), nil)
}

// RulesAPIKey is the key cost controls are read with: the enforcement key, or
// the metering key when it is unset. Empty when neither is set, so the
// enforcement engine can fall back to REVENIUM_ENFORCEMENT_API_KEY.
func (c *ReveniumConfig) RulesAPIKey() string {
	if c.EnforcementAPIKey != "" {
		return c.EnforcementAPIKey
	}
	return c.APIKey
}

// RulesBaseURL is the address cost controls are read from: the enforcement
// address, or the resolved metering address when it is unset. Empty when
// neither is set, so the enforcement engine can fall back to
// REVENIUM_ENFORCEMENT_BASE_URL and then to the default address.
func (c *ReveniumConfig) RulesBaseURL() string {
	if c.EnforcementBaseURL != "" {
		return c.EnforcementBaseURL
	}
	if strings.TrimSpace(c.BaseURL) == "" {
		return ""
	}
	return ResolveReveniumBaseURL(c.BaseURL)
}

// RulesAddress is the form of a rules address the enforcement engine
// registers and polls: without a trailing slash or a trailing /profitstream,
// the dev workaround documented while the path prefix was missing from the
// rules URL, so such configs don't request /profitstream twice now that the
// prefix is built in.
func RulesAddress(baseURL string) string {
	return strings.TrimSuffix(strings.TrimSuffix(baseURL, "/"), "/profitstream")
}

// sameRulesAddress folds the legacy metering suffixes before comparing,
// because the repeat-Initialize check reads the environment raw while the
// first setup's environment value was normalized when it was loaded.
func sameRulesAddress(a, b string) bool {
	return RulesAddress(NormalizeReveniumBaseURL(a)) == RulesAddress(NormalizeReveniumBaseURL(b))
}

func requestedAccount(active, named *ReveniumConfig) *ReveniumConfig {
	if named == nil {
		named = &ReveniumConfig{}
	}
	env := readReveniumEnv()
	pick := func(setting accountSettings, namedValue, envValue string) string {
		if value := strings.TrimSpace(namedValue); value != "" {
			return value
		}
		if active.fromEnv&setting != 0 {
			return envValue
		}
		return ""
	}
	return &ReveniumConfig{
		APIKey:             pick(settingAPIKey, named.APIKey, env.APIKey),
		BaseURL:            pick(settingBaseURL, named.BaseURL, env.BaseURL),
		TeamID:             pick(settingTeamID, named.TeamID, env.TeamID),
		EnforcementAPIKey:  pick(settingEnforcementAPIKey, named.EnforcementAPIKey, env.EnforcementAPIKey),
		EnforcementBaseURL: pick(settingEnforcementBaseURL, named.EnforcementBaseURL, env.EnforcementBaseURL),
	}
}
