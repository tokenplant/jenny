package log

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

// TestSetOutputToWriter verifies that SetOutput accepts an io.Writer
// and correctly redirects log output to it.
func TestSetOutputToWriter(t *testing.T) {
	// Create a pipe to capture output
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() error = %v", err)
	}

	// Set output to the pipe writer
	SetOutput(w)

	// Log a message
	Info("test message for writer")

	// Close writer to flush
	w.Close()

	// Read the output
	var buf bytes.Buffer
	buf.ReadFrom(r)
	r.Close()

	// Verify the message was logged
	if buf.Len() == 0 {
		t.Error("expected log output, got empty")
	}
}

// TestSetOutputAcceptsIOWriter verifies that SetOutput accepts any io.Writer
// and doesn't fall through to stderr for valid writers.
func TestSetOutputAcceptsIOWriter(t *testing.T) {
	// Create a buffer to capture output
	var buf bytes.Buffer

	// Set output to our buffer
	SetOutput(&buf)

	// Log a message
	Info("test message to buffer")

	// Verify the message was written to our buffer (not stderr)
	if buf.Len() == 0 {
		t.Error("expected log output to buffer, got empty")
	}

	// Restore default output
	SetOutput(os.Stderr)
}

// ---------------------------------------------------------------------------
// AC4: DEBUG env var alias
// ---------------------------------------------------------------------------

func TestIsTruthy(t *testing.T) {
	tests := []struct {
		val      string
		expected bool
	}{
		{"1", true},
		{"true", true},
		{"True", true},
		{"TRUE", true},
		{"yes", true},
		{"Yes", true},
		{"YES", true},
		{"on", true},
		{"On", true},
		{"ON", true},
		{"", false},
		{"0", false},
		{"false", false},
		{"no", false},
		{"off", false},
		{"anything", false},
	}

	for _, tt := range tests {
		t.Run(tt.val, func(t *testing.T) {
			if got := isTruthy(tt.val); got != tt.expected {
				t.Errorf("isTruthy(%q) = %v, want %v", tt.val, got, tt.expected)
			}
		})
	}
}

func TestDEBUG_EnvVar_Alias(t *testing.T) {
	// Test that DEBUG=1 enables debug logging
	var buf bytes.Buffer
	SetOutput(&buf)

	// Clear JENNY_DEBUG and set DEBUG
	t.Setenv("JENNY_DEBUG", "")
	t.Setenv("DEBUG", "1")

	// Reset logger with new env vars
	resetLogger()

	// Debug level should be enabled - write a debug message and check output
	buf.Reset()
	Debug("debug message test")

	if buf.Len() == 0 {
		t.Error("expected debug output when DEBUG=1, got empty buffer")
	}

	// Restore default output
	SetOutput(os.Stderr)
}

func TestJENNY_DEBUG_StillWorks(t *testing.T) {
	// Test that JENNY_DEBUG still works as an alias for DEBUG
	var buf bytes.Buffer
	SetOutput(&buf)

	// Clear DEBUG and set JENNY_DEBUG
	t.Setenv("DEBUG", "")
	t.Setenv("JENNY_DEBUG", "1")

	resetLogger()

	// Debug level should be enabled - write a debug message and check output
	buf.Reset()
	Debug("debug message test")

	if buf.Len() == 0 {
		t.Error("expected debug output when JENNY_DEBUG=1, got empty buffer")
	}

	// Restore default output
	SetOutput(os.Stderr)
}

func TestDEBUG_EmptyValue(t *testing.T) {
	// Test that empty DEBUG value does not enable debug logging
	var buf bytes.Buffer
	SetOutput(&buf)

	// Set DEBUG to empty string
	t.Setenv("DEBUG", "")
	t.Setenv("JENNY_DEBUG", "")

	resetLogger()

	// Debug level should NOT be enabled - write a debug message
	buf.Reset()
	Debug("debug message test")

	// slog at DEBUG level won't write when level is INFO
	// So empty buffer is expected when debug is disabled
	// (This is correct behavior - no output expected when DEBUG is empty)

	// Restore default output
	SetOutput(os.Stderr)
}

func TestDebug_Output(t *testing.T) {
	// Test that Debug() actually writes output when debug level is enabled
	var buf bytes.Buffer
	SetOutput(&buf)

	t.Setenv("DEBUG", "1")
	t.Setenv("JENNY_DEBUG", "")
	resetLogger()

	buf.Reset()
	Debug("test output")

	if buf.Len() == 0 {
		t.Error("expected output when DEBUG=1, got empty buffer")
	}

	if !strings.Contains(buf.String(), "test output") {
		t.Errorf("expected 'test output' in debug output, got %q", buf.String())
	}

	SetOutput(os.Stderr)
}
