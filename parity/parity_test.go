// Package parity_test contains declarative blackbox parity tests
// that verify jenny's behavior against reference agent specifications.
//
// Tests are organized by feature area across multiple files:
//   - cli_test.go          — CLI flags, exit codes, error handling
//   - stream_json_test.go  — NDJSON output format and event shapes
//   - api_protocol_test.go — API request conformance
//   - system_prompt_test.go — System prompt assembly and overrides
//   - tool_call_test.go    — Tool execution, error handling, concurrency
//   - cost_tracking_test.go — Usage and cost fields in terminal result
//   - session_test.go      — Session persistence and resume
//   - normalization_test.go — Message normalization and repair
package parity_test
