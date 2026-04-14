package core

import (
	"os"
	"path/filepath"

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

// IsValidAPIKeyFormat checks if the API key has a valid format
func IsValidAPIKeyFormat(key string) bool {
	// Revenium API keys should start with "hak_"
	if len(key) < 4 {
		return false
	}
	return key[:4] == "hak_"
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

// LoadEnvFiles loads environment variables from .env files.
// It searches the current directory and parent directories for .env and .env.local files.
func LoadEnvFiles() {
	envFiles := []string{
		".env.local", // Local overrides (highest priority)
		".env",       // Main env file
	}

	cwd, err := os.Getwd()
	if err != nil {
		cwd = "."
	}

	searchDirs := []string{
		cwd,
		filepath.Dir(cwd),
		filepath.Join(cwd, ".."),
	}

	for _, dir := range searchDirs {
		for _, envFile := range envFiles {
			envPath := filepath.Join(dir, envFile)
			if _, err := os.Stat(envPath); err == nil {
				_ = godotenv.Load(envPath)
			}
		}
	}
}
