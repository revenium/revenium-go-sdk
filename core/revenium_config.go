package core

import "os"

type ReveniumConfig struct {
	APIKey         string
	BaseURL        string
	OrgID          string
	ProductID      string
	TeamID         string
	LogLevel       string
	Debug          bool
	VerboseStartup bool
}

func LoadReveniumConfig() *ReveniumConfig {
	baseURL := GetEnvOrDefault(EnvBaseURL, DefaultBaseURL)

	return &ReveniumConfig{
		APIKey:         os.Getenv(EnvAPIKey),
		BaseURL:        NormalizeReveniumBaseURL(baseURL),
		OrgID:          os.Getenv(EnvOrgID),
		ProductID:      os.Getenv(EnvProductID),
		TeamID:         os.Getenv(EnvTeamID),
		LogLevel:       GetEnvOrDefault(EnvLogLevel, DefaultLogLevel),
		Debug:          os.Getenv(EnvDebug) == "true" || os.Getenv(EnvDebug) == "1",
		VerboseStartup: os.Getenv(EnvVerboseStartup) == "true" || os.Getenv(EnvVerboseStartup) == "1",
	}
}

func MergeReveniumConfig(programmatic, env *ReveniumConfig) *ReveniumConfig {
	if programmatic == nil {
		return env
	}
	if programmatic.APIKey == "" {
		programmatic.APIKey = env.APIKey
	}
	if programmatic.BaseURL == "" {
		programmatic.BaseURL = env.BaseURL
	}
	if programmatic.OrgID == "" {
		programmatic.OrgID = env.OrgID
	}
	if programmatic.ProductID == "" {
		programmatic.ProductID = env.ProductID
	}
	if programmatic.TeamID == "" {
		programmatic.TeamID = env.TeamID
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
	return programmatic
}

func ValidateReveniumConfig(cfg *ReveniumConfig) error {
	if cfg == nil {
		return NewConfigError("ReveniumConfig is nil", nil)
	}
	if cfg.APIKey == "" {
		return NewConfigError("REVENIUM_METERING_API_KEY is required", nil)
	}
	if !IsValidAPIKeyFormat(cfg.APIKey) {
		return NewConfigError("REVENIUM_METERING_API_KEY must start with 'hak_' or 'rev_'", nil)
	}
	return nil
}
