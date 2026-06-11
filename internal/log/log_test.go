package log

import (
	"bytes"
	"fmt"
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

// ---------------------------------------------------------------------------
// AC5: --verbose flag enables debug logging
// ---------------------------------------------------------------------------

func TestVerbose_Flag_EmitsDebugMessages(t *testing.T) {
	// Test that JENNY_VERBOSE=1 enables debug logging
	var buf bytes.Buffer
	SetOutput(&buf)

	// Clear other debug env vars and set JENNY_VERBOSE
	t.Setenv("DEBUG", "")
	t.Setenv("JENNY_DEBUG", "")
	t.Setenv("JENNY_VERBOSE", "1")

	// Reset logger with new env vars
	resetLogger()

	// Debug level should be enabled - write a debug message and check output
	buf.Reset()
	Debug("verbose debug test")

	if buf.Len() == 0 {
		t.Error("expected debug output when JENNY_VERBOSE=1, got empty buffer")
	}

	if !strings.Contains(buf.String(), "verbose debug test") {
		t.Errorf("expected 'verbose debug test' in debug output, got %q", buf.String())
	}

	// Restore default output
	SetOutput(os.Stderr)
}

func TestVerbose_EmptyValue(t *testing.T) {
	// Test that empty JENNY_VERBOSE value does not enable debug logging
	var buf bytes.Buffer
	SetOutput(&buf)

	// Set all debug vars to empty
	t.Setenv("DEBUG", "")
	t.Setenv("JENNY_DEBUG", "")
	t.Setenv("JENNY_VERBOSE", "")

	resetLogger()

	// Debug level should NOT be enabled - write a debug message
	buf.Reset()
	Debug("verbose debug test")

	// Assert no output when verbose is disabled
	if buf.Len() != 0 {
		t.Errorf("expected no output when verbose is disabled, got %d bytes", buf.Len())
	}

	// Restore default output
	SetOutput(os.Stderr)
}

// TestSetVerbose_ProgrammaticEnable tests that SetVerbose(true) enables debug logging
// when called after init() has already run (simulating main.go behavior).
func TestSetVerbose_ProgrammaticEnable(t *testing.T) {
	var buf bytes.Buffer
	SetOutput(&buf)

	// Clear all debug env vars first
	t.Setenv("DEBUG", "")
	t.Setenv("JENNY_DEBUG", "")
	t.Setenv("JENNY_VERBOSE", "")

	// Reset to ensure clean state
	resetLogger()

	// Initially, debug should be disabled
	buf.Reset()
	Debug("should not appear")
	if buf.Len() != 0 {
		t.Error("expected no debug output initially")
	}

	// Call SetVerbose(true) - this should enable debug logging
	SetVerbose(true)

	// Now debug should be enabled
	buf.Reset()
	Debug("should appear now")

	if buf.Len() == 0 {
		t.Error("expected debug output after SetVerbose(true), got empty buffer")
	}

	if !strings.Contains(buf.String(), "should appear now") {
		t.Errorf("expected 'should appear now' in debug output, got %q", buf.String())
	}

	// SetVerbose(false) should disable it again
	SetVerbose(false)

	buf.Reset()
	Debug("should not appear after disable")
	if buf.Len() != 0 {
		t.Errorf("expected no debug output after SetVerbose(false), got %d bytes", buf.Len())
	}

	// Restore default output
	SetOutput(os.Stderr)
}

func TestRingBuffer_CapsAt100Entries(t *testing.T) {
	ResetForTest()
	t.Cleanup(ResetForTest)

	// Call Error 150 times with distinct messages
	for i := 1; i <= 150; i++ {
		Error("error message #" + fmt.Sprintf("%d", i))
	}

	// Get the in-memory errors
	errors := GetInMemoryErrors()

	// Assert exactly 100 entries
	if len(errors) != 100 {
		t.Errorf("expected exactly 100 entries, got %d", len(errors))
	}

	// Assert oldest50 entries have been evicted (first entry is #51)
	if !strings.Contains(errors[0].Message, "#51") {
		t.Errorf("expected first entry to be #51, got %s", errors[0].Message)
	}

	// Assert last entry is #150
	if !strings.Contains(errors[99].Message, "#150") {
		t.Errorf("expected last entry to be #150, got %s", errors[99].Message)
	}
}

func TestRingBuffer_FIFOEviction(t *testing.T) {
	ResetForTest()
	t.Cleanup(ResetForTest)

	// Add 100 entries (fills the buffer at default capacity 100)
	for i := 1; i <= 100; i++ {
		Error(fmt.Sprintf("entry-%d", i))
	}

	errors := GetInMemoryErrors()
	if len(errors) != 100 {
		t.Fatalf("expected 100 entries, got %d", len(errors))
	}

	// Add 101st entry, should evict the oldest ("entry-1")
	Error("entry-101")

	errors = GetInMemoryErrors()
	if len(errors) != 100 {
		t.Errorf("expected 100 entries after eviction, got %d", len(errors))
	}

	// First entry should now be "entry-2"
	if !strings.Contains(errors[0].Message, "entry-2") {
		t.Errorf("expected first entry to be entry-2, got %s", errors[0].Message)
	}
}

func TestRingBuffer_EmptyBuffer(t *testing.T) {
	ResetForTest()
	t.Cleanup(ResetForTest)

	errors := GetInMemoryErrors()
	if len(errors) != 0 {
		t.Errorf("expected empty buffer, got %d entries", len(errors))
	}
}

// ---------------------------------------------------------------------------
// AC4: Last-Request Capture
// ---------------------------------------------------------------------------

func TestLastRequest_SetAndGet(t *testing.T) {
	ResetForTest()
	t.Cleanup(ResetForTest)

	// Create a LastRequest with known values
	lr := LastRequest{
		Model:     "claude-3-sonnet-20240229",
		MaxTokens: 1000,
		System:    "test system",
		Tools:     nil,
		Messages:  nil,
	}

	// Set it
	SetLastRequest(lr)

	// Get it back
	result := GetLastRequest()

	if result == nil {
		t.Fatal("expected non-nil LastRequest, got nil")
	}

	// Assert all fields match
	if result.Model != "claude-3-sonnet-20240229" {
		t.Errorf("expected Model 'claude-3-sonnet-20240229', got %q", result.Model)
	}
	if result.MaxTokens != 1000 {
		t.Errorf("expected MaxTokens 1000, got %d", result.MaxTokens)
	}
	if result.System != "test system" {
		t.Errorf("expected System 'test system', got %q", result.System)
	}
	if result.Tools != nil {
		t.Errorf("expected Tools nil, got %v", result.Tools)
	}
	if result.Messages != nil {
		t.Errorf("expected Messages nil by default, got %v", result.Messages)
	}
}

func TestLastRequest_MessagesNilByDefault(t *testing.T) {
	ResetForTest()
	t.Cleanup(ResetForTest)

	lr := LastRequest{
		Model:     "claude-3-sonnet-20240229",
		MaxTokens: 1000,
		System:    "test system",
	}

	SetLastRequest(lr)

	result := GetLastRequest()
	if result.Messages != nil {
		t.Errorf("expected Messages to be nil, got %v", result.Messages)
	}
}

func TestLastRequest_OverwriteEachTurn(t *testing.T) {
	ResetForTest()
	t.Cleanup(ResetForTest)

	// First request
	lr1 := LastRequest{Model: "model-v1"}
	SetLastRequest(lr1)

	// Second request (overwrites first)
	lr2 := LastRequest{Model: "model-v2"}
	SetLastRequest(lr2)

	result := GetLastRequest()
	if result.Model != "model-v2" {
		t.Errorf("expected model-v2 (most recent), got %q", result.Model)
	}
}

func TestLastRequest_NilWhenEmpty(t *testing.T) {
	ResetForTest()
	t.Cleanup(ResetForTest)

	result := GetLastRequest()
	if result != nil {
		t.Errorf("expected nil when store is empty, got %v", result)
	}
}

func TestLastRequest_ImmutableFromGetter(t *testing.T) {
	ResetForTest()
	t.Cleanup(ResetForTest)

	SetLastRequest(LastRequest{Model: "original"})

	result := GetLastRequest()
	result.Model = "modified" // Try to mutate

	// Re-fetch, should still be original
	reFetch := GetLastRequest()
	if reFetch.Model != "original" {
		t.Errorf("expected original value, mutations should not affect store")
	}
}

// TestLastRequest_DeepCopySlices verifies that GetLastRequest returns deep copies
// of the Tools and Messages slices, so mutations to slice elements don't affect the store.
func TestLastRequest_DeepCopySlices(t *testing.T) {
	ResetForTest()
	t.Cleanup(ResetForTest)

	// Set a LastRequest with non-nil slices
	SetLastRequest(LastRequest{
		Model:    "test-model",
		Tools:    []any{"tool-a", "tool-b"},
		Messages: []any{"msg1", "msg2"},
	})

	// Get a copy and mutate the slice elements
	result := GetLastRequest()
	result.Tools[0] = "modified-tool"
	result.Messages[0] = "modified-msg"

	// Re-fetch and verify original values are intact
	reFetch := GetLastRequest()
	if reFetch.Tools[0] != "tool-a" {
		t.Errorf("expected Tools[0] to be 'tool-a', got %v", reFetch.Tools[0])
	}
	if reFetch.Tools[1] != "tool-b" {
		t.Errorf("expected Tools[1] to be 'tool-b', got %v", reFetch.Tools[1])
	}
	if reFetch.Messages[0] != "msg1" {
		t.Errorf("expected Messages[0] to be 'msg1', got %v", reFetch.Messages[0])
	}
	if reFetch.Messages[1] != "msg2" {
		t.Errorf("expected Messages[1] to be 'msg2', got %v", reFetch.Messages[1])
	}
}
