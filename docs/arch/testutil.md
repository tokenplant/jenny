---
title: Test Utilities
slug: testutil
priority: P3
status: done
spec: complete
code: done
package: internal/testutil, internal/testutil/mockapi, internal/agent
depends_on: []
---
# Test Utilities

## Overview

`internal/testutil` is a shared test helper package that provides utilities used by
tests across multiple internal packages. It exists as a neutral location to avoid
import cycles between packages that need the same helpers.

## Purpose

The package was introduced to break the import cycle between `internal/agent` and
`internal/tool`: both packages needed `CaptureStdout` and `SSELine` helpers, but
neither could import the other. Moving these helpers to a neutral package
(`internal/testutil`) that neither `agent` nor `tool` depends on resolved the cycle.

## API: internal/testutil

### CaptureStdout

```go
func CaptureStdout(t *testing.T, fn func()) string
```

Redirects `os.Stdout` to a pipe for the duration of `fn` and returns everything
written. Uses a background goroutine to drain the pipe, avoiding deadlocks when
`fn` produces large output. Calls `t.Helper()`.

### SSELine

```go
func SSELine(event, data string) string
```

Formats a Server-Sent Events (SSE) line in the format:
`event: <event>\ndata: <data>\n\n`. Used by agent streaming tests to construct
mock SSE responses.

## API: internal/testutil/mockapi

The `mockapi` subpackage provides an in-process mock server for the Anthropic API, serving responses from "cassette" files.

### NewMockServer

```go
func NewMockServer(cassetteDir string) *MockServer
```

Starts a new mock server that serves cassettes from the specified directory. The server listens on a random port.

### MockServer.URL

```go
func (m *MockServer) URL() string
```

Returns the base URL of the mock server (e.g., `http://127.0.0.1:56789`).

### MockServer.Close

```go
func (m *MockServer) Close()
```

Stops the mock server and releases the port.

### MockServer.Requests

```go
func (m *MockServer) Requests() []APIRequest
```

Returns a slice of all requests captured by the server. Each `APIRequest` contains the decoded JSON body and the HTTP headers.

### MockServer.ClearRequests

```go
func (m *MockServer) ClearRequests()
```

Resets the internal list of captured requests.

## API: internal/agent (Test Helpers)

The following helpers are defined in `internal/agent/testhelpers_test.go` for inspecting agent output in tests.

### parseAssistantEvents

```go
func parseAssistantEvents(output string) ([]any, error)
```

Parses the NDJSON output of an agent session and extracts the content items from `assistant_message` events.

### parseNDJSONLines

```go
func parseNDJSONLines(output string) ([]map[string]any, error)
```

Parses a string containing multiple JSON objects (one per line) into a slice of maps.

### hasTextWith

```go
func hasTextWith(content []any, want string) bool
```

Reports whether any element of the content slice is a text block whose text matches `want`.

### hasToolUseWithID

```go
func hasToolUseWithID(content []any, want string) bool
```

Reports whether any element of the content slice is a tool_use block with the specified ID.

## Usage

### Using MockServer

```go
s := mockapi.NewMockServer("testdata/cassettes")
defer s.Close()

// Use s.URL() as the base URL for the API client
client := api.NewClient(s.URL())
```

### Content Inspection

```go
content, _ := parseAssistantEvents(output)
if !hasTextWith(content, "Hello, world!") {
    t.Error("expected greeting")
}
```

