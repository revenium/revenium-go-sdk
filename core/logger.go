package core

import (
	"fmt"
	"log"
	"os"
	"strings"
	"time"
)

// LogLevel represents the logging level
type LogLevel int

const (
	LogLevelDebug LogLevel = iota
	LogLevelInfo
	LogLevelWarn
	LogLevelError
)

// String returns the string representation of the log level
func (l LogLevel) String() string {
	switch l {
	case LogLevelDebug:
		return "DEBUG"
	case LogLevelInfo:
		return "INFO"
	case LogLevelWarn:
		return "WARN"
	case LogLevelError:
		return "ERROR"
	default:
		return "UNKNOWN"
	}
}

// Logger interface defines the logging methods
type Logger interface {
	Debug(message string, args ...interface{})
	Info(message string, args ...interface{})
	Warn(message string, args ...interface{})
	Error(message string, args ...interface{})
	SetLevel(level LogLevel)
	GetLevel() LogLevel
}

// DefaultLogger is the default console logger implementation
type DefaultLogger struct {
	level LogLevel
}

// NewDefaultLogger creates a new default logger
func NewDefaultLogger() *DefaultLogger {
	return &DefaultLogger{
		level: LogLevelInfo,
	}
}

// SetLevel sets the logging level
func (l *DefaultLogger) SetLevel(level LogLevel) {
	l.level = level
}

// GetLevel returns the current logging level
func (l *DefaultLogger) GetLevel() LogLevel {
	return l.level
}

// Debug logs a debug message
func (l *DefaultLogger) Debug(message string, args ...interface{}) {
	if l.level <= LogLevelDebug {
		l.log("DEBUG", message, args...)
	}
}

// Info logs an info message
func (l *DefaultLogger) Info(message string, args ...interface{}) {
	if l.level <= LogLevelInfo {
		l.log("INFO", message, args...)
	}
}

// Warn logs a warning message
func (l *DefaultLogger) Warn(message string, args ...interface{}) {
	if l.level <= LogLevelWarn {
		l.log("WARN", message, args...)
	}
}

// Error logs an error message
func (l *DefaultLogger) Error(message string, args ...interface{}) {
	if l.level <= LogLevelError {
		l.log("ERROR", message, args...)
	}
}

// log is the internal logging method
func (l *DefaultLogger) log(level, message string, args ...interface{}) {
	timestamp := time.Now().Format("2006-01-02 15:04:05")
	prefix := fmt.Sprintf("[%s] [Revenium %s]", timestamp, level)

	if len(args) > 0 {
		message = fmt.Sprintf(message, args...)
	}

	log.Printf("%s %s", prefix, message)
}

// Global logger instance
var globalLogger Logger = NewDefaultLogger()

// GetLogger returns the global logger instance
func GetLogger() Logger {
	return globalLogger
}

// SetLogger sets a custom global logger
func SetLogger(logger Logger) {
	globalLogger = logger
}

// InitializeLogger initializes the logger from environment variables
func InitializeLogger() {
	logLevelStr := strings.ToUpper(os.Getenv("REVENIUM_LOG_LEVEL"))
	level := ParseLogLevel(logLevelStr)
	globalLogger.SetLevel(level)

	if os.Getenv("REVENIUM_VERBOSE_STARTUP") == "true" || os.Getenv("REVENIUM_VERBOSE_STARTUP") == "1" {
		globalLogger.Info("Logger initialized with level: %s", level.String())
	}
}

// SetGlobalDebug enables or disables debug logging globally.
// This provides backward compatibility with providers that use a boolean debug flag.
func SetGlobalDebug(enabled bool) {
	if enabled {
		globalLogger.SetLevel(LogLevelDebug)
	}
}

// ParseLogLevel parses a string log level to LogLevel
func ParseLogLevel(level string) LogLevel {
	switch strings.ToUpper(level) {
	case "DEBUG":
		return LogLevelDebug
	case "INFO":
		return LogLevelInfo
	case "WARN", "WARNING":
		return LogLevelWarn
	case "ERROR":
		return LogLevelError
	default:
		return LogLevelInfo
	}
}

// Convenience functions for global logger

func Debug(message string, args ...interface{}) {
	globalLogger.Debug(message, args...)
}

func Info(message string, args ...interface{}) {
	globalLogger.Info(message, args...)
}

func Warn(message string, args ...interface{}) {
	globalLogger.Warn(message, args...)
}

func Error(message string, args ...interface{}) {
	globalLogger.Error(message, args...)
}
