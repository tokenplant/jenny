// Package log provides structured logging via slog.
package log

import (
	"log/slog"
	"os"
)

// Logger is the package-level logger instance.
var Logger *slog.Logger

func init() {
	// Set up slog to write to stderr (default)
	opts := &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}

	// JENNY_DEBUG=1 enables debug-level logging
	if os.Getenv("JENNY_DEBUG") != "" {
		opts.Level = slog.LevelDebug
	}

	Logger = slog.New(slog.NewTextHandler(os.Stderr, opts))
}

// Debug logs a debug-level message.
func Debug(msg string, args ...any) {
	Logger.Debug(msg, args...)
}

// Info logs an info-level message.
func Info(msg string, args ...any) {
	Logger.Info(msg, args...)
}

// Warn logs a warning-level message.
func Warn(msg string, args ...any) {
	Logger.Warn(msg, args...)
}

// Error logs an error-level message.
func Error(msg string, args ...any) {
	Logger.Error(msg, args...)
}
