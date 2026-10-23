package core

import (
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

const DefaultReveniumBaseURL = DefaultBaseURL

// GetEnvOrDefault gets an environment variable or returns a default value
func GetEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// MeteringKeyPrefix starts a metering key, which sends usage records and
// tool events but which Revenium refuses for job and outcome calls.
const MeteringKeyPrefix = "rev_mk_"

// IsMeteringAPIKey reports whether key is a metering key.
func IsMeteringAPIKey(key string) bool {
	return strings.HasPrefix(key, MeteringKeyPrefix)
}

// IsValidAPIKeyFormat checks if the API key has a valid format
func IsValidAPIKeyFormat(key string) bool {
	if len(key) < 4 {
		return false
	}
	prefix := key[:4]
	return prefix == "hak_" || prefix == "rev_"
}

// NormalizeReveniumBaseURL normalizes the base URL to a consistent format.
// It handles various input formats and returns a normalized base URL without trailing slash.
// The endpoint path (/meter/v2/ai/completions) is appended by sendMeteringRequest.
func NormalizeReveniumBaseURL(baseURL string) string {
	if baseURL == "" {
		return DefaultReveniumBaseURL
	}

	// Remove trailing slash if present
	if len(baseURL) > 0 && baseURL[len(baseURL)-1] == '/' {
		baseURL = baseURL[:len(baseURL)-1]
	}

	// If it already ends with /meter/v2, remove /meter/v2 (legacy format)
	if len(baseURL) >= 9 && baseURL[len(baseURL)-9:] == "/meter/v2" {
		return baseURL[:len(baseURL)-9]
	}

	// If it ends with /meter, remove /meter (legacy format)
	if len(baseURL) >= 6 && baseURL[len(baseURL)-6:] == "/meter" {
		return baseURL[:len(baseURL)-6]
	}

	// If it ends with /v2, remove /v2 (legacy format)
	if len(baseURL) >= 3 && baseURL[len(baseURL)-3:] == "/v2" {
		return baseURL[:len(baseURL)-3]
	}

	// Return the base URL as-is (should be just the domain)
	return baseURL
}

var envFileNames = []string{".env.local", ".env"}

// LoadEnvFiles copies the variables in .env.local and then .env from the
// working directory into the process environment, without overriding a
// variable that is already set. No other directory is read, because a file
// placed above the working directory could point the provider and Revenium
// addresses, and with them the API keys, at another server. Setting
// REVENIUM_ENV_FILES_DISABLE to true turns loading off.
func LoadEnvFiles() {
	if IsEnvFlagSet(EnvEnvFilesDisable) {
		return
	}
	for _, name := range envFileNames {
		if _, err := os.Stat(name); err == nil {
			_ = godotenv.Load(name)
		}
	}
}

// IsEnvFlagSet reports whether a boolean REVENIUM_* setting is on: "true"
// in any letter case, or "1", ignoring surrounding spaces. Every flag the SDK
// reads from the environment is parsed here.
func IsEnvFlagSet(name string) bool {
	value := strings.TrimSpace(os.Getenv(name))
	return value == "1" || strings.EqualFold(value, "true")
}

// DefaultBufferMaxBytes bounds the usage records a client holds in memory:
// the records held while Revenium is unreachable and the records waiting for
// their first send, even while Revenium is healthy. A plain record measures about 1.2 KB held, so
// 20,000 of them (about 24 MB) stay inside it and the record count remains the
// limit without prompt capture. A captured record carries three fields of up to
// REVENIUM_MAX_PROMPT_SIZE characters, about 150 KB at the default, so this
// keeps roughly 700 of those instead of letting 20,000 reach 3 GB. The Python
// SDK must use the same value.
const DefaultBufferMaxBytes int64 = 100 << 20

// BufferMaxBytes returns REVENIUM_BUFFER_MAX_BYTES as a whole number of bytes,
// or DefaultBufferMaxBytes when it is unset or not a positive whole number.
func BufferMaxBytes() int64 {
	if limit := envBufferMaxBytes(); limit > 0 {
		return limit
	}
	if strings.TrimSpace(os.Getenv(EnvBufferMaxBytes)) != "" {
		Warn("%s must be a positive whole number of bytes; using %d", EnvBufferMaxBytes, DefaultBufferMaxBytes)
	}
	return DefaultBufferMaxBytes
}

// envBufferMaxBytes returns REVENIUM_BUFFER_MAX_BYTES as a positive whole
// number of bytes, or 0 when it is unset or not one. It never warns, because
// LoadReveniumConfig runs it on every load; BufferMaxBytes warns once when a
// client is built.
func envBufferMaxBytes() int64 {
	limit, err := strconv.ParseInt(strings.TrimSpace(os.Getenv(EnvBufferMaxBytes)), 10, 64)
	if err != nil || limit <= 0 {
		return 0
	}
	return limit
}
