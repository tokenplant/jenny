# Parity Test Suite

Blackbox end-to-end test suite for comparing jenny behavior with reference agent compatibility targets.

## Overview

The parity suite verifies that jenny produces behavior compatible with the reference agent across:

- CLI flag parsing and behavior
- API protocol conformance
- Stream-json output format
- System prompt assembly
- Tool call flows
- Cost and token tracking
- Session persistence and resume
- Message normalization

## Directory Structure

```
parity/
├── README.md                  # This file
├── parity_test.go             # Package doc
├── helpers_test.go            # Shared test helpers
├── cli_test.go                # CLI flags, exit codes, error handling
├── stream_json_test.go        # NDJSON output format and event shapes
├── api_protocol_test.go       # API request conformance
├── system_prompt_test.go      # System prompt assembly and overrides
├── tool_call_test.go          # Tool execution, error handling, concurrency
├── cost_tracking_test.go      # Usage and cost fields in terminal result
├── session_test.go            # Session persistence and resume
├── normalization_test.go      # Message normalization and repair
├── fixtures/cassettes/        # SSE cassettes for mock API replay
└── harness/                   # Test infrastructure
    ├── types.go               # Type definitions
    ├── runner.go              # Binary spawner
    ├── mock_api.go            # Mock Anthropic API server
    ├── comparator.go          # Result diffing (exit code, stdout/stderr, API requests, stream-json)
    ├── reporter.go            # Output formatters
    └── suite.go               # Test orchestration
```

## Running the Suite

From the repo root:

```bash
go test ./parity/... -v
```

Run a specific test category:

```bash
go test ./parity/... -v -run TestCLI
go test ./parity/... -v -run TestStreamJSON
go test ./parity/... -v -run TestAPIProtocol
go test ./parity/... -v -run TestSystemPrompt
go test ./parity/... -v -run TestToolCall
go test ./parity/... -v -run TestCostTracking
go test ./parity/... -v -run TestSession
go test ./parity/... -v -run TestNormalization
```

## Test Coverage Summary

| Category | Tests | What it covers |
|----------|-------|----------------|
| `cli-flags` | 17 | Version, help, no prompt, unknown flags, output format, resume, print-system-prompt, verbose, no-session-persistence |
| `api-protocol` | 10 | max_tokens=64000, system prompt placement, tool definitions, model field, messages format, tool_result pairing, system prompt content |
| `stream-json` | 16 | NDJSON validity, envelope fields (session_id, uuid), init event, result event (usage, duration, cost, stop_reason, num_turns), assistant event, event sequence, tool_call events, user tool_result wrapping |
| `system-prompt` | 21 | Default identity, bash safety, search tools, minimum length, no unfilled templates, date/platform/cwd injection, custom/append overrides, CLAUDE.md/AGENTS.md loading and precedence, tool list |
| `tool-call` | 8 | Basic bash flow, unknown tool error, text+tool mixed, multiple tools, parallel reads, max-iterations, empty stop_reason |
| `cost-tracking` | 10 | usage object, input/output/cache tokens, total_cost_usd, duration_ms/api_ms, num_turns, modelUsage, multi-turn accumulation |
| `session` | 7 | Session ID format, consistency, --no-session-persistence, resume errors, fork-session validation |
| `normalization` | 5 | System prompt as top-level, tool pairing, unknown tool pairing, stdout purity, verbose isolation |

## Test Case Structure

Test cases are defined as `harness.TestCase` structs with:

- **TargetInvocation**: How to invoke jenny (cli, prompt with cassette, subprocess)
- **ExpectedBehavior**: Assertions on exit code, stdout/stderr, API requests, stream-json events

## Assertion Capabilities

### Output assertions (`StdoutExpectation` / `StderrExpectation`)
- `Equals`: exact match
- `Contains`: at least one substring matches (OR semantics)
- `NotContains`: all substrings must be absent
- `Matches`: regex patterns against individual lines
- `Length`: min/max/exact byte length constraints
- `IsEmpty`: output must be empty

### API request assertions (`APIRequestExpectation`)
- `Model`: exact model name or regex
- `MaxTokens`: expected max_tokens value
- `HasSystemPrompt`: system prompt exists
- `System`: system prompt substring checks
- `Tools`: tool definition checks (count, names, fields)
- `HasField` / `FieldEquals`: generic request body checks

### Stream-JSON assertions (`StreamJSONExpectation`)
- `AllLinesValidJSON`: every line parses as JSON
- `FirstEvent` / `LastEvent`: check specific events
- `HasEventTypes`: assert event type presence
- `SessionIDConsistent`: all events share session_id
- `UUIDsUnique`: no duplicate uuids
- `EventCount`: min/max event constraints
- `EventAssertions`: per-event checks by index or type filter

## Mock API Server

The harness includes a mock API server that:

- Intercepts requests to `ANTHROPIC_BASE_URL`
- Serves SSE cassettes for response replay
- Captures request bodies for assertion
- Supports multi-turn sequences via `CassetteSequence`
- Clears requests between test cases for isolation

## Reporters

- **TextReporter**: Human-readable output with pass/fail indicators
- **JSONReporter**: JSON lines for machine parsing

## Related

- [E2E Test Harness](../docs/arch/e2e-test-harness.md)
- [CLI Documentation](../docs/arch/cli.md)
- [Stream JSON Spec](../docs/arch/stream-json-spec.md)
