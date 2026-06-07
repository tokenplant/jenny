// Package log provides structured logging via slog.
package log

import (
	"log/slog"
	"os"
)

// Logger is the package-level logger instance.
var Logger *slog.Logger

// outputWriter controls where log output is sent.
var outputWriter any = os.Stderr

func init() {
	resetLogger()
}

func resetLogger() {
	opts := &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}

	// JENNY_DEBUG=1 enables debug-level logging
	if os.Getenv("JENNY_DEBUG") != "" {
		opts.Level = slog.LevelDebug
	}

	w := outputWriter
	if w == nil {
		w = os.Stderr
	}

	// w is either io.Writer or *os.File
	switch v := w.(type) {
	case *os.File:
		Logger = slog.New(slog.NewTextHandler(v, opts))
	default:
		// Fallback for io.Writer
		Logger = slog.New(slog.NewTextHandler(os.Stderr, opts))
	}
}

// SetOutput redirects log output to the specified writer.
// This is used to redirect debug logs to stderr when stream-json mode is active.
func SetOutput(w any) {
	outputWriter = w
	resetLogger()
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
