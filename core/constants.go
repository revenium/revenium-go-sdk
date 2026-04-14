package core

const (
	DefaultBaseURL  = "https://api.revenium.ai"
	DefaultLogLevel = "INFO"

	EnvAPIKey         = "REVENIUM_METERING_API_KEY"
	EnvBaseURL        = "REVENIUM_METERING_BASE_URL"
	EnvOrgID          = "REVENIUM_ORGANIZATION_ID"
	EnvProductID      = "REVENIUM_PRODUCT_ID"
	EnvLogLevel       = "REVENIUM_LOG_LEVEL"
	EnvTeamID         = "REVENIUM_TEAM_ID"
	EnvDebug          = "REVENIUM_DEBUG"
	EnvVerboseStartup = "REVENIUM_VERBOSE_STARTUP"

	EnvCapturePrompts = "REVENIUM_CAPTURE_PROMPTS"
	EnvMaxPromptSize  = "REVENIUM_MAX_PROMPT_SIZE"
	EnvPrintSummary   = "REVENIUM_PRINT_SUMMARY"
)
