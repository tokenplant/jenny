// Package log provides structured logging via slog.
package log

import (
	"io"
	"log/slog"
	"os"
	"strings"
)

// Logger is the package-level logger instance.
var Logger *slog.Logger

// outputWriter controls where log output is sent.
var outputWriter io.Writer = os.Stderr

func init() {
	resetLogger()
}

func resetLogger() {
	opts := &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}

	// DEBUG=1 or JENNY_DEBUG=1 enables debug-level logging
	if isTruthy(os.Getenv("DEBUG")) || isTruthy(os.Getenv("JENNY_DEBUG")) {
		opts.Level = slog.LevelDebug
	}

	w := outputWriter
	if w == nil {
		w = os.Stderr
	}

	Logger = slog.New(slog.NewTextHandler(w, opts))
}

// isTruthy returns true if the given string represents a truthy value.
// Matches Claude Code's behavior: "1", "true", "yes", "on" (case-insensitive).
func isTruthy(val string) bool {
	switch strings.ToLower(val) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

// SetOutput redirects log output to the specified writer.
// This is used to redirect debug logs to stderr when stream-json mode is active.
func SetOutput(w io.Writer) {
	outputWriter = w
	resetLogger()
}

// Output returns the current output writer. Used for testing.
func Output() io.Writer {
	return outputWriter
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
