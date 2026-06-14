// Package harness provides blackbox end-to-end test infrastructure for e2e
// testing of jenny behavior.
//
// The harness supports:
//   - Spawning the target binary with configurable environment
//   - Mock API server for replaying cassettes
//   - Capturing stdout, stderr, exit code, API requests, and transcript entries
//   - Configurable via e2e.Config
package harness

import "testing"

// Config holds e2e test configuration.
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

// TestCase represents a single e2e test case.
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
	// WorkDirFiles are files to create in the temp work dir before running.
	// Keys are relative paths, values are file contents.
	WorkDirFiles map[string]string
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
	// CassetteSequence for multi-turn prompt tests: ordered cassette IDs
	// served in sequence for the same cassette endpoint.
	CassetteSequence []string
	// Env adds per-case environment variables (merged with suite env).
	Env []string
	// Name for tool kind.
	ToolName string
	// Input for tool kind.
	ToolInput any
	// MockBehavior configures custom rules for the mock server.
	MockBehavior *MockBehavior
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
	// StreamJSON are assertions on parsed NDJSON output events.
	StreamJSON *StreamJSONExpectation
	// TranscriptEntries are assertions on transcript entries.
	TranscriptEntries []TranscriptEntryExpectation
	// FileSystem are assertions on file system state.
	FileSystem []FileSystemExpectation
}

// StreamJSONExpectation specifies assertions on NDJSON output events.
type StreamJSONExpectation struct {
	// AllLinesValidJSON asserts every non-empty stdout line is valid JSON.
	AllLinesValidJSON bool
	// FirstEvent checks the first NDJSON event.
	FirstEvent *EventExpectation
	// LastEvent checks the last NDJSON event.
	LastEvent *EventExpectation
	// HasEventTypes asserts these event types appear at least once.
	HasEventTypes []string
	// SessionIDConsistent asserts all events share the same session_id.
	SessionIDConsistent bool
	// UUIDsUnique asserts every uuid is distinct.
	UUIDsUnique bool
	// EventCount specifies expected event count constraints.
	EventCount *LengthExpectation
	// EventAssertions are per-event custom assertions (index-based).
	EventAssertions []IndexedEventExpectation
	// CompareToReference compares each NDJSON line against the reference binary's output.
	CompareToReference bool
}

// EventExpectation specifies assertions on a single NDJSON event.
type EventExpectation struct {
	// Type is the expected "type" field.
	Type string
	// Subtype is the expected "subtype" field.
	Subtype string
	// HasFields asserts these top-level keys exist.
	HasFields []string
	// FieldEquals asserts specific field values.
	FieldEquals map[string]any
	// FieldContains asserts field value contains substring (string fields only).
	FieldContains map[string]string
	// FieldNotEmpty asserts these fields are non-empty.
	FieldNotEmpty []string
	// Nested checks a nested object field.
	Nested map[string]*EventExpectation
}

// IndexedEventExpectation ties an EventExpectation to an event index or type filter.
type IndexedEventExpectation struct {
	// Index is the 0-based event index (-1 means match by Type filter).
	Index int
	// TypeFilter matches the first event with this type (used when Index < 0).
	TypeFilter string
	// SubtypeFilter narrows TypeFilter matches.
	SubtypeFilter string
	// Expect is the assertion.
	Expect EventExpectation
}

// StdoutExpectation specifies stdout assertions.
type StdoutExpectation struct {
	// Equals is the exact expected stdout.
	Equals string
	// Contains are substrings; at least one must appear (OR semantics).
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
	// Index is the 0-based request index to check (-1 means any request).
	Index int
	// Model is the expected model (or regex).
	Model string
	// MaxTokens is the expected max_tokens value (0 = don't check).
	MaxTokens int
	// HasSystemPrompt asserts the request has a non-empty system prompt.
	HasSystemPrompt bool
	// System is the system prompt expectation.
	System *SystemExpectation
	// Messages are message expectations.
	Messages []MessageExpectation
	// Tools are assertions on tool definitions in the request.
	Tools *ToolsExpectation
	// HasField asserts the request body has specific top-level keys.
	HasField []string
	// FieldEquals asserts specific field values in request body.
	FieldEquals map[string]any
}

// ToolsExpectation specifies assertions on tool definitions.
type ToolsExpectation struct {
	// MinCount is the minimum number of tools expected.
	MinCount int
	// HasTool asserts a tool with this name is present.
	HasTool []string
	// NotHasTool asserts a tool with this name is absent.
	NotHasTool []string
	// EachHasFields asserts every tool has these top-level keys.
	EachHasFields []string
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
	// Path is the exact path relative to work dir. Mutually exclusive with Pattern.
	Path string
	// Pattern is a glob pattern relative to work dir. Mutually exclusive with Path.
	Pattern string
	// ExpectedCount is the exact number of files expected to match Pattern.
	ExpectedCount int
	// Content is the expected exact content of the file (used with Path).
	Content string
	// MustNotExist indicates the file (or pattern matches) must not exist.
	MustNotExist bool
	// JSONL specifies assertions for JSONL file content.
	JSONL *JSONLExpectation
}

// JSONLExpectation specifies assertions for a JSONL file.
type JSONLExpectation struct {
	// AllLinesValidJSON asserts every non-empty line is valid JSON.
	AllLinesValidJSON bool
	// RequiredFields asserts that every line contains these top-level keys.
	RequiredFields []string
	// HasTypes asserts these event types appear at least once across the lines.
	HasTypes []string
	// SessionIDMatchesStdout asserts that the UUID stem of the file matches
	// the session_id found in the stdout stream-json events.
	SessionIDMatchesStdout bool
	// AllLinesHaveUUID asserts that every line has a valid UUID v4 in "uuid" field.
	AllLinesHaveUUID bool
	// MinCount asserts a minimum number of valid JSON lines.
	MinCount int
	// MaxCount asserts a maximum number of valid JSON lines.
	MaxCount int
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
	// ReferenceDiff contains field-by-field differences between jenny and reference binary output.
	ReferenceDiff []string
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

// E2ETB is a testing.TB interface for test helpers.
type E2ETB interface {
	testing.TB
	Fatal(args ...any)
	Fatalf(format string, args ...any)
	Skip(args ...any)
	Skipf(format string, args ...any)
}
