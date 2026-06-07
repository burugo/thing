package thing

import (
	"io"

	"github.com/burugo/thing/internal/logging"
)

// LogLevel controls Thing library log verbosity.
type LogLevel = logging.Level

const (
	LogDefault LogLevel = logging.Default
	LogSilent  LogLevel = logging.Silent
	LogError   LogLevel = logging.Error
	LogWarn    LogLevel = logging.Warn
	LogInfo    LogLevel = logging.Info
	LogDebug   LogLevel = logging.Debug
)

// Logger is the minimal interface required for Thing logging.
type Logger = logging.Logger

// SetLogger sets the global Thing logger. Passing nil disables Thing logs.
func SetLogger(logger Logger) {
	logging.SetLogger(logger)
}

// SetLogOutput sends Thing logs to w. Passing nil disables Thing logs.
func SetLogOutput(w io.Writer) {
	logging.SetOutput(w)
}

// SetLogLevel sets the global Thing log level.
func SetLogLevel(level LogLevel) {
	logging.SetLevel(level)
}
