package core

import "strings"

// ResolveReveniumBaseURL returns the Revenium address a standalone client
// sends to: the one it was configured with, else REVENIUM_METERING_BASE_URL,
// else the default. Usage records, tool events and job calls built from the
// same settings therefore reach the same Revenium.
func ResolveReveniumBaseURL(configured string) string {
	if trimmed := strings.TrimSpace(configured); trimmed != "" {
		return NormalizeReveniumBaseURL(trimmed)
	}
	return LoadReveniumConfig().BaseURL
}

// ResolveValidReveniumBaseURL resolves the address as ResolveReveniumBaseURL
// does and checks it. An invalid value is reported under configuredSetting
// when the caller passed it, and under REVENIUM_METERING_BASE_URL when it
// came from the environment.
func ResolveValidReveniumBaseURL(configured, configuredSetting string) (string, error) {
	setting := EnvBaseURL
	if strings.TrimSpace(configured) != "" {
		setting = configuredSetting
	}
	baseURL := ResolveReveniumBaseURL(configured)
	if err := ValidateReveniumBaseURL(setting, baseURL); err != nil {
		return "", err
	}
	return baseURL, nil
}
