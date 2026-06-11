---
title: E2E Test Harness
slug: e2e-test-harness
priority: P0
status: complete
spec: complete
code: complete
package: e2e
depends_on:
  - cli.md
  - stream-json-spec.md
  - anthropic-api-client.md
---
# E2E Test Harness

## Overview

Blackbox end-to-end test suite for jenny. The suite launches the compiled
`jenny` binary as a subprocess, drives it with CLI flags and stdin, and
asserts on its stdout, stderr, exit code, and the HTTP traffic it emits
against an in-process mock server. Tests are organized as Go test
functions under `e2e/`. No live API access is required.

## Directory Layout

```
e2e/
├── harness/                       # shared test infrastructure
│   ├── runner.go                  # jenny binary builder + spawner (RunJenny, RunTarget)
│   ├── mock_api.go                # mock Anthropic API server
│   ├── types.go                   # TestCase, ExpectedBehavior, etc.
│   ├── comparator.go             # declarative comparison engine
│   ├── suite.go                  # declarative SuiteRunner
│   └── reporter.go              # TextReporter / JSONReporter
├── fixtures/cassettes/           # SSE cassette files
├── cli_test.go                   # CLI flags
├── stream_json_test.go           # stream-json envelope
├── api_protocol_test.go          # API request shape
├── system_prompt_test.go         # system prompt assembly
├── tool_call_test.go             # tool call flows
├── tools_test.go                 # per-tool behavior
├── skill_plugin_test.go          # skills/plugin discovery
├── cost_tracking_test.go         # cost/usage
├── session_test.go               # session persistence
├── normalization_test.go         # message normalization
├── transcript_test.go            # transcript file tests
└── e2e_test.go                   # top-level test definitions
```

All mock server, runner, and comparison infrastructure is consolidated
in `e2e/harness/`.

## System Prompt Verification

The `--print-system-prompt` flag allows verifying the assembled system prompt
without making any API calls. This is used by `e2e/system_prompt_test.go`
to assert on the presence of core tools, platform context, and overall
structure. This flag runs entirely offline and exits before any network or
session initialization.

## Cassette File Format

Cassettes are plain SSE text files. One file per API exchange. The mock
server streams the file verbatim as `Content-Type: text/event-stream`,
byte-for-byte, with no transformation. Consumers parse the SSE blocks
just as they would parse a live Anthropic streaming response.

Naming convention: `<cassette-id>.sse`, all lowercase, hyphen-separated,
unique across the suite. The cassette id is the only thing the mock
server needs to find a file; it is taken from the URL path prefix
`/cassette/<id>/v1/messages`.

## Mock Server

The mock server is started per-test via `harness.NewMockServer(cassetteDir)`.
It returns an `*httptest.Server` plus a `*MockServer` handle for inspecting
captured requests.

Captured requests:

Each POST appends a decoded copy of the request body to the mock
server's in-memory list. Tests call `Requests()` to retrieve a copy and
assert against the outbound request shape (model, stream flag, messages,
tools, etc.).

### Cassette sequences (multi-turn)

Single-turn flows are the common case, but tool use is a multi-turn
pattern: the model responds with `stop_reason: "tool_use"`, jenny runs
the tool, then makes a second `/v1/messages` call carrying the tool
result. The mock server supports this via per-cassette-id sequences:

```go
mock.SetSequence("tool-use", []string{"tool-use-turn1", "tool-use-turn2"})
```

## Binary Runner

The runner builds the jenny binary once per `go test` invocation using
`go build -o <tmpdir>/jenny ./cmd/jenny` and caches the result with
`sync.Once`.

`harness.RunJenny(t, env, args...)` returns a `RunResult`:

```go
type RunResult struct {
    Lines      []string         // raw stdout lines (newline-split)
    Parsed     []map[string]any // lines parsed as JSON (blanks skipped)
    Stdout     string           // raw stdout
    Stderr     string           // captured stderr
    ExitCode   int              // process exit code
    Dir        string           // working directory of the process
    DurationMs int64            // total execution time
}
```

## Running the Suite

From the repo root:

```bash
go test ./e2e/...
```

The suite is hermetic: with `ANTHROPIC_AUTH_TOKEN` and
`ANTHROPIC_BASE_URL` unset in the environment, the mock server is the
sole destination of all HTTP traffic, and no network access is
required.

## Acceptance Criteria

### API protocol conformance (api_protocol_test.go)

- **AC1 — `max_tokens` is 64000:** the captured request has a numeric
  `max_tokens` field equal to 64000.
- **AC2 — `system` field is present and substantial:** the request body
  has a top-level `system` field.
- **AC3 — `system` prompt includes the working directory:** the system
  prompt content contains the absolute path of the directory from which
  the jenny subprocess is spawned.
- **AC4 — `tools` array is present and non-empty:** the request body
  has a `tools` key whose value is a JSON array with at least one
  element.
- **AC5 — core tools present by name:** the `tools` array always
  includes tools named `"Bash"` and `"Read"`.

## go fix Constraint: e2e/harness

`go fix ./e2e/harness/...` requires multiple consecutive runs and exits 1.
This is a documented constraint of the test infrastructure.
