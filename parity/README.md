# Parity Test Suite

Blackbox end-to-end test suite for comparing jenny behavior with reference agent compatibility targets.

## Overview

The parity suite is designed to verify that jenny produces behavior compatible with the reference agent across:

- CLI flag parsing and behavior
- API protocol conformance
- Stream-json output format
- Session resume and continuation
- Tool call flows
- System prompt assembly

## Directory Structure

```
parity/
├── README.md              # This file
├── parity_test.go         # Main test runner
└── harness/ # Test infrastructure
    ├── types.go           # Type definitions
    ├── runner.go          # Binary spawner
    ├── mock_api.go        # Mock Anthropic API server
    ├── comparator.go      # Result diffing
    ├── reporter.go        # Output formatters
    └── suite.go           # Test orchestration
```

## Running the Suite

From the repo root:

```bash
go test ./parity/... -v
```

## Test Case Structure

Test cases are defined as `harness.TestCase` structs:

```go
{
    ID:          "cli.version.flag",
    Category:    "cli-flags",
    Description: "--version prints version and exits 0",
    Tags:        []string{"smoke"},
    Target: harness.TargetInvocation{
        Kind: "cli",
        Args: []string{"--version"},
    },
    Expected: harness.ExpectedBehavior{
        ExitCode: 0,
        Stdout: &harness.StdoutExpectation{
            Matches: []string{`^\d+\.\d+\.\d+`},
        },
    },
}
```

## Categories

| Category | Description |
|----------|-------------|
| `cli-flags` | CLI flag parsing and behavior |
| `api-protocol` | Outbound API request conformance |
| `stream-json` | NDJSON output format |
| `session-resume` | Session resume and continuation |
| `tool-call` | Tool use flows |
| `system-prompt` | System prompt assembly |

## Target Invocation

The `TargetInvocation` supports multiple invocation styles:

- **cli**: Direct CLI arguments
- **prompt**: Run with a prompt (requires cassette for mock API)
- **subprocess**: Arbitrary subprocess command

## Expectations

`ExpectedBehavior` supports assertions on:

- **ExitCode**: Expected process exit code
- **Stdout/Stderr**: Output content assertions (contains, matches, length)
- **APIRequests**: Captured API request body assertions
- **TranscriptEntries**: Transcript file entry assertions
- **FileSystem**: File system state assertions

## Mock API Server

The harness includes a mock API server that:

- Intercepts requests to `ANTHROPIC_BASE_URL`
- Serves SSE cassettes for response replay
- Captures request bodies for assertion
- Supports multi-turn sequences via `SetSequence`

## Reporter

The suite supports multiple reporter formats:

- **TextReporter** (default): Human-readable output
- **JSONReporter**: JSON lines for machine parsing

## Comparison with Reference

This suite is inspired by reference agent parity testing but implemented in Go for native jenny testing.

Key differences:

- Go-based test harness
- Reuses existing jenny_test harness infrastructure
- Native Go test integration via `go test`
- Mock API server from jenny_test/harness

## Related

- [E2E Test Harness](../docs/e2e-test-harness.md)
- [CLI Documentation](../docs/cli.md)
- [Stream JSON Spec](../docs/stream-json-spec.md)
