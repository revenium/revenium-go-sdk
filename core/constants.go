package core

const (
	DefaultBaseURL  = "https://api.revenium.ai"
	DefaultLogLevel = "INFO"

	EnforcementRulesPathPrefix = "/profitstream/v2/api/ai/enforcement-rules/"

	ProviderNameAnthropic = "ANTHROPIC"

	EnvAPIKey             = "REVENIUM_METERING_API_KEY"
	EnvBaseURL            = "REVENIUM_METERING_BASE_URL"
	EnvOrgID              = "REVENIUM_ORGANIZATION_ID"
	EnvProductID          = "REVENIUM_PRODUCT_ID"
	EnvLogLevel           = "REVENIUM_LOG_LEVEL"
	EnvTeamID             = "REVENIUM_TEAM_ID"
	EnvEnforcementBaseURL = "REVENIUM_ENFORCEMENT_BASE_URL"
	EnvEnforcementAPIKey  = "REVENIUM_ENFORCEMENT_API_KEY"
	EnvDebug              = "REVENIUM_DEBUG"
	EnvVerboseStartup     = "REVENIUM_VERBOSE_STARTUP"
	EnvEnvFilesDisable    = "REVENIUM_ENV_FILES_DISABLE"

	EnvCapturePrompts       = "REVENIUM_CAPTURE_PROMPTS"
	EnvCaptureErrorMessages = "REVENIUM_CAPTURE_ERROR_MESSAGES"
	EnvMaxPromptSize        = "REVENIUM_MAX_PROMPT_SIZE"
	EnvPrintSummary         = "REVENIUM_PRINT_SUMMARY"
	EnvBufferMaxBytes       = "REVENIUM_BUFFER_MAX_BYTES"
)
