// Package log provides structured logging via slog.
package log

import (
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"
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

	// JENNY_DEBUG=1 or JENNY_VERBOSE=1 or DEBUG=1 enables debug-level logging
	if isTruthy(os.Getenv("DEBUG")) || isTruthy(os.Getenv("JENNY_DEBUG")) || isTruthy(os.Getenv("JENNY_VERBOSE")) {
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

// SetVerbose enables or disables debug-level logging.
// This is called from main.go after command-line flag parsing,
// since the --verbose flag is set after log.init() runs.
func SetVerbose(verbose bool) {
	if verbose {
		os.Setenv("JENNY_VERBOSE", "1")
	} else {
		os.Unsetenv("JENNY_VERBOSE")
	}
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
	errorRing.Append(ErrorEntry{Time: time.Now(), Message: msg, Args: args})
	Logger.Error(msg, args...)
}

// ErrorEntry represents a single error entry in the ring buffer.
type ErrorEntry struct {
	Time    time.Time
	Message string
	Args    []any
}

// errorRing is the bounded FIFO ring buffer for error entries (capacity 100).
var errorRing = ringBuffer{capacity: 100}

// ringBuffer implements a bounded FIFO buffer with capacity 100.
type ringBuffer struct {
	mu       sync.RWMutex
	entries  []ErrorEntry
	capacity int
}

// Append adds an entry to the ring buffer, evicting the oldest if at capacity.
func (rb *ringBuffer) Append(entry ErrorEntry) {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	if len(rb.entries) >= rb.capacity {
		// Evict oldest entry (shift left)
		rb.entries = rb.entries[1:]
	}
	rb.entries = append(rb.entries, entry)
}

// GetAll returns a copy of all entries in the ring buffer.
func (rb *ringBuffer) GetAll() []ErrorEntry {
	rb.mu.RLock()
	defer rb.mu.RUnlock()

	result := make([]ErrorEntry, len(rb.entries))
	copy(result, rb.entries)
	return result
}

// GetInMemoryErrors returns all error entries from the ring buffer.
func GetInMemoryErrors() []ErrorEntry {
	return errorRing.GetAll()
}

// LastRequest represents the most recent API request parameters.
type LastRequest struct {
	Model     string
	MaxTokens int
	System    string
	Tools     []any // Using []any for flexibility; callers cast as needed
	Messages  []any // Nil by default; only populated for internal debug
}

// lastRequestStore holds the most recent API request parameters.
var lastRequestStore *LastRequest

// lastRequestMu protects concurrent access to lastRequestStore.
var lastRequestMu sync.RWMutex

// SetLastRequest stores the given LastRequest as the most recent API request.
func SetLastRequest(lr LastRequest) {
	lastRequestMu.Lock()
	defer lastRequestMu.Unlock()
	lastRequestStore = &lr
}

// GetLastRequest returns the most recent API request parameters, or nil if none.
func GetLastRequest() *LastRequest {
	lastRequestMu.RLock()
	defer lastRequestMu.RUnlock()
	if lastRequestStore == nil {
		return nil
	}
	// Return a deep copy to prevent external mutation of slices
	result := *lastRequestStore
	if result.Tools != nil {
		result.Tools = append([]any(nil), result.Tools...)
	}
	if result.Messages != nil {
		result.Messages = append([]any(nil), result.Messages...)
	}
	return &result
}

// ResetForTest resets all global state for testing. Use with t.Cleanup to restore state.
func ResetForTest() {
	errorRing.mu.Lock()
	errorRing.entries = nil
	errorRing.capacity = 100
	errorRing.mu.Unlock()
	lastRequestMu.Lock()
	lastRequestStore = nil
	lastRequestMu.Unlock()
}
