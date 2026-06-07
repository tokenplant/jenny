package log

import (
	"bytes"
	"os"
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
