package core

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLogLevel_String(t *testing.T) {
	tests := []struct {
		level    LogLevel
		expected string
	}{
		{LogLevelDebug, "DEBUG"},
		{LogLevelInfo, "INFO"},
		{LogLevelWarn, "WARN"},
		{LogLevelError, "ERROR"},
		{LogLevel(99), "UNKNOWN"},
	}

	for _, tt := range tests {
		assert.Equal(t, tt.expected, tt.level.String())
	}
}

func TestParseLogLevel(t *testing.T) {
	tests := []struct {
		input    string
		expected LogLevel
	}{
		{"DEBUG", LogLevelDebug},
		{"debug", LogLevelDebug},
		{"INFO", LogLevelInfo},
		{"WARN", LogLevelWarn},
		{"WARNING", LogLevelWarn},
		{"ERROR", LogLevelError},
		{"", LogLevelInfo},
		{"unknown", LogLevelInfo},
	}

	for _, tt := range tests {
		result := ParseLogLevel(tt.input)
		assert.Equal(t, tt.expected, result, "ParseLogLevel(%q)", tt.input)
	}
}

func TestDefaultLogger_LevelControl(t *testing.T) {
	logger := NewDefaultLogger()

	assert.Equal(t, LogLevelInfo, logger.GetLevel())

	logger.SetLevel(LogLevelDebug)
	assert.Equal(t, LogLevelDebug, logger.GetLevel())

	logger.SetLevel(LogLevelError)
	assert.Equal(t, LogLevelError, logger.GetLevel())
}

func TestSetGlobalDebug(t *testing.T) {
	// Save and restore
	original := globalLogger
	defer func() { globalLogger = original }()

	logger := NewDefaultLogger()
	globalLogger = logger

	assert.Equal(t, LogLevelInfo, logger.GetLevel())

	SetGlobalDebug(true)
	assert.Equal(t, LogLevelDebug, logger.GetLevel())

	// SetGlobalDebug(false) should not change level (only enables, doesn't disable)
	SetGlobalDebug(false)
	assert.Equal(t, LogLevelDebug, logger.GetLevel())
}

func TestGetSetLogger(t *testing.T) {
	original := GetLogger()
	defer SetLogger(original)

	custom := NewDefaultLogger()
	custom.SetLevel(LogLevelError)

	SetLogger(custom)
	assert.Equal(t, custom, GetLogger())
}

func TestInitializeLogger(t *testing.T) {
	original := globalLogger
	defer func() { globalLogger = original }()

	globalLogger = NewDefaultLogger()

	originalEnv := os.Getenv("REVENIUM_LOG_LEVEL")
	defer os.Setenv("REVENIUM_LOG_LEVEL", originalEnv)

	os.Setenv("REVENIUM_LOG_LEVEL", "DEBUG")
	InitializeLogger()
	assert.Equal(t, LogLevelDebug, globalLogger.GetLevel())

	os.Setenv("REVENIUM_LOG_LEVEL", "ERROR")
	InitializeLogger()
	assert.Equal(t, LogLevelError, globalLogger.GetLevel())
}
