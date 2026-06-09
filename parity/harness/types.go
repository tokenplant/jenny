// Package harness provides blackbox end-to-end test infrastructure for parity
// testing of jenny behavior.
//
// The harness supports:
//   - Spawning the target binary with configurable environment
//   - Mock API server for replaying cassettes
//   - Capturing stdout, stderr, exit code, API requests, and transcript entries
//   - Configurable via parity.Config
package harness

import "testing"

// Config holds parity test configuration.
type Config struct {
	// ProductName is the name of the product under test (e.g. "jenny").
	ProductName string
	// Target is the path to the target binary or "node /path/to/cli.js" style command.
	Target string
	// TargetArgs are args to pass before any test-specific args.
	TargetArgs []string
	// CassetteDir is the directory containing SSE cassette files.
	CassetteDir string
	// TempDir is the temp dir for test runs. Defaults to os.TempDir().
	TempDir string
	// TimeoutMs is the timeout per test in ms. Default: 60000.
	TimeoutMs int
	// Verbose enables verbose logging.
	Verbose bool
}

// TestCase represents a single parity test case.
type TestCase struct {
	// ID is the unique identifier for the test case.
	ID string
	// Category is the test category (e.g. "cli-flags", "api-protocol").
	Category string
	// Description is the human-readable description.
	Description string
	// Tags are tags for filtering.
	Tags []string
	// Target is the invocation specification.
	Target TargetInvocation
	// Expected is the expected behavior.
	Expected ExpectedBehavior
	// Skip indicates the test should be skipped.
	Skip *SkipCondition
}

// TargetInvocation specifies how to invoke the target.
type TargetInvocation struct {
	// Kind is "cli", "prompt", "tool", or "subprocess".
	Kind string
	// Args for cli/subprocess kind.
	Args []string
	// Prompt for prompt kind.
	Prompt string
	// Format for prompt kind: "stream-json" or "text".
	Format string
	// Cassette for prompt kind.
	Cassette string
	// Name for tool kind.
	ToolName string
	// Input for tool kind.
	ToolInput any
}

// ExpectedBehavior holds assertions on the target's behavior.
type ExpectedBehavior struct {
	// ExitCode is the expected process exit code.
	ExitCode int
	// Stdout is the expected stdout assertions.
	Stdout *StdoutExpectation
	// Stderr is the expected stderr assertions.
	Stderr *StderrExpectation
	// APIRequests are assertions on API requests.
	APIRequests []APIRequestExpectation
	// TranscriptEntries are assertions on transcript entries.
	TranscriptEntries []TranscriptEntryExpectation
	// FileSystem are assertions on file system state.
	FileSystem []FileSystemExpectation
}

// StdoutExpectation specifies stdout assertions.
type StdoutExpectation struct {
	// Equals is the exact expected stdout.
	Equals string
	// Contains are substrings that must appear.
	Contains []string
	// NotContains are substrings that must NOT appear.
	NotContains []string
	// Matches are regexes that must match.
	Matches []string
	// Length specifies length constraints.
	Length *LengthExpectation
	// IsEmpty indicates stdout should be empty.
	IsEmpty bool
}

// LengthExpectation specifies length constraints.
type LengthExpectation struct {
	Min   int
	Max   int
	Exact int
}

// StderrExpectation specifies stderr assertions (same as stdout).
type StderrExpectation = StdoutExpectation

// APIRequestExpectation specifies API request assertions.
type APIRequestExpectation struct {
	// Model is the expected model (or regex).
	Model string
	// System is the system prompt expectation.
	System *SystemExpectation
	// Messages are message expectations.
	Messages []MessageExpectation
}

// SystemExpectation specifies system prompt assertions.
type SystemExpectation struct {
	// Contains are substrings that must appear.
	Contains []string
	// NotContains are substrings that must NOT appear.
	NotContains []string
}

// MessageExpectation specifies a message assertion.
type MessageExpectation struct {
	// Role is the expected role ("user" or "assistant").
	Role string
	// Content is the expected content (or substring).
	Content any
}

// TranscriptEntryExpectation specifies a transcript entry assertion.
type TranscriptEntryExpectation struct {
	// Type is the expected entry type.
	Type string
	// MustContain are key-value pairs that must be present.
	MustContain map[string]any
}

// FileSystemExpectation specifies a file system assertion.
type FileSystemExpectation struct {
	// Path is the path relative to work dir.
	Path string
	// Content is the expected content.
	Content string
	// MustNotExist indicates the file must not exist.
	MustNotExist bool
}

// SkipCondition specifies skip conditions.
type SkipCondition struct {
	// Reason is the skip reason.
	Reason string
	// Unless is a feature flag.
	Unless string
	// Platforms are allowed platforms.
	Platforms []string
}

// TestResult represents the result of a test case.
type TestResult struct {
	ID         string
	Category   string
	Status     string // "pass", "fail", "skip", "error"
	Duration   int64  // milliseconds
	Message    string
	Diff       []DiffDetail
	Actual     *CapturedOutput
	SkipReason string
}

// DiffDetail represents a single difference.
type DiffDetail struct {
	Path     string
	Expected any
	Actual   any
	Message  string
}

// CapturedOutput holds all captured output from a test run.
type CapturedOutput struct {
	ExitCode int
	Stdout   string
	Stderr   string
	Requests []RecordedRequest
}

// RecordedRequest is a captured API request.
type RecordedRequest struct {
	Body map[string]any
}

// ParityTB is a testing.TB interface for test helpers.
type ParityTB interface {
	testing.TB
	Fatal(args ...any)
	Fatalf(format string, args ...any)
	Skip(args ...any)
	Skipf(format string, args ...any)
}
