package logging

import (
	"io"
	"log"
	"os"
	"strings"
	"sync"
)

// Level controls Thing library log verbosity.
type Level int

const (
	Default Level = iota
	Silent
	Error
	Warn
	Info
	Debug
)

// Logger is the minimal interface required for Thing logging.
type Logger interface {
	Printf(format string, args ...interface{})
}

var state = struct {
	sync.RWMutex
	logger Logger
	level  Level
}{
	logger: log.New(os.Stderr, "", log.LstdFlags),
	level:  Default,
}

func SetLogger(logger Logger) {
	state.Lock()
	defer state.Unlock()
	state.logger = logger
}

func SetOutput(w io.Writer) {
	if w == nil {
		SetLogger(nil)
		return
	}
	SetLogger(log.New(w, "", log.LstdFlags))
}

func SetLevel(level Level) {
	state.Lock()
	defer state.Unlock()
	state.level = level
}

func Debugf(format string, args ...interface{}) { logf(Debug, format, args...) }
func Infof(format string, args ...interface{})  { logf(Info, format, args...) }
func Warnf(format string, args ...interface{})  { logf(Warn, format, args...) }
func Errorf(format string, args ...interface{}) { logf(Error, format, args...) }

func Printf(format string, args ...interface{}) { logf(levelForMessage(format), format, args...) }
func Println(args ...interface{}) {
	Printf(strings.TrimSpace(strings.Repeat("%v ", len(args))), args...)
}

func logf(level Level, format string, args ...interface{}) {
	state.RLock()
	logger := state.logger
	configuredLevel := state.level
	state.RUnlock()
	if logger == nil || !enabled(configuredLevel, level) {
		return
	}
	logger.Printf(format, args...)
}

func enabled(configuredLevel Level, messageLevel Level) bool {
	if configuredLevel == Default {
		configuredLevel = Warn
	}
	if configuredLevel == Silent {
		return false
	}
	return messageLevel <= configuredLevel
}

func levelForMessage(message string) Level {
	trimmed := strings.TrimSpace(message)
	upper := strings.ToUpper(trimmed)
	switch {
	case strings.HasPrefix(upper, "WARN") || strings.HasPrefix(upper, "[WARN]") || strings.HasPrefix(upper, "WARNING"):
		return Warn
	case strings.HasPrefix(upper, "ERROR") || strings.HasPrefix(upper, "[ERROR]"):
		return Error
	case strings.HasPrefix(upper, "INFO"):
		return Info
	case strings.HasPrefix(upper, "DEBUG") || strings.HasPrefix(upper, "[DEBUG]"):
		return Debug
	case strings.HasPrefix(upper, "DB ") || strings.HasPrefix(upper, "CACHE ") || strings.Contains(upper, " SQL"):
		return Debug
	default:
		return Info
	}
}
