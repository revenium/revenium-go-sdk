package core

import (
	"math"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	EnvOrganizationName    = "REVENIUM_ORGANIZATION_NAME"
	EnvProductName         = "REVENIUM_PRODUCT_NAME"
	EnvEnvironment         = "REVENIUM_ENVIRONMENT"
	EnvRegion              = "REVENIUM_REGION"
	EnvTraceType           = "REVENIUM_TRACE_TYPE"
	EnvTraceName           = "REVENIUM_TRACE_NAME"
	EnvParentTransactionID = "REVENIUM_PARENT_TRANSACTION_ID"
	EnvTransactionName     = "REVENIUM_TRANSACTION_NAME"
	EnvCredentialAlias     = "REVENIUM_CREDENTIAL_ALIAS"
	EnvRetryNumber         = "REVENIUM_RETRY_NUMBER"
	EnvAPITimeout          = "REVENIUM_API_TIMEOUT"
	EnvTicketID            = "REVENIUM_TICKET_ID"
	EnvAgenticJobID        = "REVENIUM_AGENTIC_JOB_ID"
	EnvAgenticJobName      = "REVENIUM_AGENTIC_JOB_NAME"
	EnvAgenticJobType      = "REVENIUM_AGENTIC_JOB_TYPE"
	EnvAgenticJobVersion   = "REVENIUM_AGENTIC_JOB_VERSION"
)

// DefaultMeteringTimeout is how long one request to Revenium may take when
// REVENIUM_API_TIMEOUT is not set.
const DefaultMeteringTimeout = 10 * time.Second

// metadataDefaultSources lists, per usage-metadata key, the environment
// variables that supply a process-wide default, first match wins. The
// fallbacks after the REVENIUM_ name match the Python SDK.
var metadataDefaultSources = []struct {
	key       string
	variables []string
}{
	{"environment", []string{EnvEnvironment, "ENVIRONMENT", "DEPLOYMENT_ENV"}},
	{"region", []string{EnvRegion, "AWS_REGION", "AWS_DEFAULT_REGION", "AZURE_REGION", "GCP_REGION", "GOOGLE_CLOUD_REGION"}},
	{"traceType", []string{EnvTraceType}},
	{"traceName", []string{EnvTraceName}},
	{"parentTransactionId", []string{EnvParentTransactionID}},
	{"transactionName", []string{EnvTransactionName}},
	{"credentialAlias", []string{EnvCredentialAlias}},
	{"ticketId", []string{EnvTicketID}},
	{"agenticJobId", []string{EnvAgenticJobID}},
	{"agenticJobName", []string{EnvAgenticJobName}},
	{"agenticJobType", []string{EnvAgenticJobType}},
	{"agenticJobVersion", []string{EnvAgenticJobVersion}},
}

// LoadMetadataDefaults returns the usage metadata the environment sets for
// every record of the process. A call's own metadata wins over it.
func LoadMetadataDefaults() map[string]interface{} {
	defaults := map[string]interface{}{}
	for _, source := range metadataDefaultSources {
		if value := firstEnvValue(source.variables...); value != "" {
			defaults[source.key] = value
		}
	}
	if retry, ok := retryNumberFromEnv(); ok {
		defaults["retryNumber"] = retry
	}
	return defaults
}

// RecordDefaults returns the values every record of a client carries when
// its own usage metadata leaves them unset: LoadMetadataDefaults plus the
// organization and product, taken from rc when set there in code and from
// the environment (as LoadReveniumConfig reads it) otherwise. The metering
// client and the cost-control check both call it with the same config, so a
// record and its cost-control decision name the same organization, product
// and credential.
func RecordDefaults(rc *ReveniumConfig) map[string]interface{} {
	env := LoadReveniumConfig()
	defaults := LoadMetadataDefaults()
	if organization := codeOrEnv(rc, func(c *ReveniumConfig) string { return c.OrgID }, env.OrgID); organization != "" {
		defaults["organizationName"] = organization
	}
	if product := codeOrEnv(rc, func(c *ReveniumConfig) string { return c.ProductID }, env.ProductID); product != "" {
		defaults["productName"] = product
	}
	return defaults
}

func codeOrEnv(rc *ReveniumConfig, setting func(*ReveniumConfig) string, envValue string) string {
	if rc != nil && !IsBlank(setting(rc)) {
		return strings.TrimSpace(setting(rc))
	}
	return strings.TrimSpace(envValue)
}

// CredentialAliasFor returns the credential name an alias gives a call: the
// alias without surrounding spaces, or "" when the alias is blank or does not
// apply. Revenium resolves a named credential before the subscriber id or
// email, so the alias is left off a subscriber that names a person and off a
// credential the caller already named or valued.
func CredentialAliasFor(alias, subscriberID, subscriberEmail string, credentialNameOrValue ...string) string {
	for _, value := range append([]string{subscriberID, subscriberEmail}, credentialNameOrValue...) {
		if !IsBlank(value) {
			return ""
		}
	}
	return strings.TrimSpace(alias)
}

func firstEnvValue(names ...string) string {
	for _, name := range names {
		if value := strings.TrimSpace(os.Getenv(name)); value != "" {
			return value
		}
	}
	return ""
}

func retryNumberFromEnv() (int, bool) {
	value := strings.TrimSpace(os.Getenv(EnvRetryNumber))
	if value == "" {
		return 0, false
	}
	retry, err := strconv.Atoi(value)
	if err != nil || retry < 0 {
		Warn("%s must be a whole number of 0 or more; records are sent without a retry number", EnvRetryNumber)
		return 0, false
	}
	return retry, true
}

// MeteringTimeout returns how long one request to Revenium may take:
// REVENIUM_API_TIMEOUT as a Go duration ("5s", "1500ms") or a number of
// seconds, or DefaultMeteringTimeout when it is unset or not a positive time.
func MeteringTimeout() time.Duration {
	value := strings.TrimSpace(os.Getenv(EnvAPITimeout))
	if value == "" {
		return DefaultMeteringTimeout
	}
	if timeout, ok := parseTimeout(value); ok {
		return timeout
	}
	Warn("%s must be a positive duration such as 5s or 1500ms; using %s", EnvAPITimeout, DefaultMeteringTimeout)
	return DefaultMeteringTimeout
}

const maxTimeoutSeconds = float64(math.MaxInt64) / float64(time.Second)

func parseTimeout(value string) (time.Duration, bool) {
	if timeout, err := time.ParseDuration(value); err == nil {
		return timeout, timeout > 0
	}
	seconds, err := strconv.ParseFloat(value, 64)
	if err != nil || math.IsNaN(seconds) || seconds <= 0 || seconds >= maxTimeoutSeconds {
		return 0, false
	}
	timeout := time.Duration(seconds * float64(time.Second))
	return timeout, timeout > 0
}
