// Package logging builds the process-wide structured logger.
//
// Logs are JSON lines on stdout so the hosting platform (CloudWatch on AWS)
// can index their fields. The level comes from LOG_LEVEL (debug, info, warn,
// error); it defaults to info.
package logging

import (
	"io"
	"log/slog"
	"os"
	"strings"
)

// New returns a JSON logger writing to w at the level named by level.
// Unknown or empty levels fall back to info.
func New(w io.Writer, level string) *slog.Logger {
	return slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{
		Level: ParseLevel(level),
	}))
}

// Setup builds the logger from LOG_LEVEL, installs it as the default for both
// log/slog and the standard log package, and returns it.
func Setup() *slog.Logger {
	logger := New(os.Stdout, os.Getenv("LOG_LEVEL"))
	slog.SetDefault(logger)
	return logger
}

// ParseLevel maps a level name to a slog.Level, defaulting to info.
func ParseLevel(name string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
