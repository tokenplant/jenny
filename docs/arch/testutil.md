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

The package was introduced to break the import cycle between `internal/agent` and
`internal/tool`: both packages needed `CaptureStdout` and `SSELine` helpers, but
neither could import the other. Moving these helpers to a neutral package
(`internal/testutil`) that neither `agent` nor `tool` depends on resolved the cycle.

The `internal/testutil/mockapi` subpackage provides an in-process mock server for the
Anthropic (and OpenAI-compatible) API, serving responses from "cassette" files. It
supports both SSE streaming and JSON one-shot responses from either provider, with
extension points for custom path handlers, request inspection, and error response
injection.

## API: internal/testutil

### CaptureStdout

```go
func CaptureStdout(t *testing.T, fn func()) string
```

Redirects `os.Stdout` to a pipe for the duration of `fn` and returns everything
written. Uses a background goroutine to drain the pipe, avoiding deadlocks when
`fn` produces large output. Calls `t.Helper()`.

Defined in `internal/testutil/helpers.go:14`.

### SSELine

```go
func SSELine(event, data string) string
```

Formats a Server-Sent Events (SSE) line in the format:
`event: <event>\ndata: <data>\n\n`. Used by agent streaming tests to construct
mock SSE responses.

Defined in `internal/testutil/helpers.go:38`.

## API: internal/testutil/mockapi -- Constructor

### Option

```go
type Option func(*MockServer)
```

`Option` is a functional-options configuration type. Callers pass options such as
`WithCassetteDir` to `NewMockServer` to configure the mock server.

### WithCassetteDir

```go
func WithCassetteDir(dir string) Option
```

`WithCassetteDir` sets the base directory from which `.sse` and `.json` cassette
files are loaded. If not set, cassette-based serving is disabled; use
`SetInlineResponse` for in-memory content only.

Follows the Go functional-options convention (with `With` prefix).

### NewMockServer

```go
func NewMockServer(opts ...Option) *MockServer
```

`NewMockServer` creates a `*MockServer` with the given options.

The server is pre-configured with:
- `POST /cassette/<id>/v1/messages` -- serves cassette files or inline content
- `GET` to that path returns `405 Method Not Allowed`
- `Content-Type: text/event-stream` (default; overridable via `SetContentType` per cassetteID)

Without a `WithCassetteDir` option, only inline responses (`SetInlineResponse`) and
custom path handlers (`SetPathHandler`) are active.

Defined in `internal/testutil/mockapi/server.go:41` (implementation).

## API: internal/testutil/mockapi -- MockServer Methods

### URL

```go
func (m *MockServer) URL() string
```

Returns the base URL of the mock server (e.g., `http://127.0.0.1:56789`).

### Close

```go
func (m *MockServer) Close()
```

Stops the mock server and releases the port.

### Requests

```go
func (m *MockServer) Requests() []APIRequest
```

Returns a copy of all requests captured by the server. Each `APIRequest` contains
the decoded JSON body and the cloned HTTP headers.

### ClearRequests

```go
func (m *MockServer) ClearRequests()
```

Resets the internal list of captured requests.

### SetSequence

```go
func (m *MockServer) SetSequence(cassetteID string, cassettes []string)
```

Registers an ordered list of cassette file names to serve sequentially for the
given `cassetteID`. Each request to that `cassetteID` serves the next cassette in
the list. Used for multi-turn conversation test scenarios.

### SetMockBehavior

```go
func (m *MockServer) SetMockBehavior(mb *MockBehavior)
```

Sets custom mock API behaviors. Currently supports `RejectEmptyToolProperties`:
when `true`, the server returns HTTP 400 if a request includes a tool definition
with an empty `input_schema.properties` map.

### SetInlineResponse (E1)

```go
func (m *MockServer) SetInlineResponse(cassetteID string, content string) *MockServer
```

Stores `content` in an in-memory map keyed by `cassetteID`. When a request arrives
for `cassetteID`, the handler checks the inline map first. If found, serves the
inline content. If not found, falls back to the file-based cassette from
`WithCassetteDir`.

- For SSE responses: `content` must be valid SSE (zero or more
  `event: <type>\ndata: <json>\n\n` lines, ending with `\n\n`).
- For JSON responses: use `SetContentType` to override the Content-Type header.
- Empty string clears the inline response.
- Returns `*MockServer` for method chaining.

**Example:**
```go
ms.SetInlineResponse("anthropic/hello", "event: message_start\ndata: {}\n\nevent: message_stop\ndata: {}\n\n")
```

### SetRequestInspector (E2)

```go
func (m *MockServer) SetRequestInspector(fn func(r APIRequest) error) *MockServer
```

Registers a callback invoked after the request body is parsed and captured, before
the response is served. The inspector receives the captured `APIRequest`. If the
callback returns a non-nil error, the server responds with HTTP 400 and the error
message as JSON `{"error": "<msg>"}`.

- Only one inspector is active at a time; registering a new one replaces the old.
- The inspector is called once per request.
- Returns `*MockServer` for method chaining.

**Example:**
```go
ms.SetRequestInspector(func(r APIRequest) error {
    if r.Header.Get("Authorization") == "" {
        return errors.New("missing Authorization header")
    }
    return nil
})
```

### SetErrorResponse (E3)

```go
func (m *MockServer) SetErrorResponse(cassetteID string, statusCode int) *MockServer
```

Registers a non-200 HTTP status code to return for a given `cassetteID`. The error
response is returned before reading any cassette content. Zero clears the override
(resets to 200 behavior).

Used for retry logic (429 rate limit), bad request (400), and BadGateway (502)
test scenarios.

Returns `*MockServer` for method chaining.

### SetContentType (E4)

```go
func (m *MockServer) SetContentType(cassetteID string, contentType string) *MockServer
```

Overrides the `Content-Type` header for a given `cassetteID`. Without this, the
default is `text/event-stream` for SSE cassettes. Use for:
- `application/json` -- JSON responses (non-streaming) from either provider
- `text/plain` -- plain text responses

Empty string clears the override (resets to `text/event-stream`).

**Note:** `SetContentType` applies only when the default `/cassette/<id>/v1/messages`
dispatcher serves the response. For custom path handlers registered via
`SetPathHandler`, the handler itself writes the `Content-Type` header directly.

Returns `*MockServer` for method chaining.

### SetPathHandler (E5)

```go
func (m *MockServer) SetPathHandler(pathPattern string, handler func(w http.ResponseWriter, r *http.Request)) *MockServer
```

Registers a custom handler for a specific HTTP path pattern. The `pathPattern`
format is `METHOD /path` (e.g., `"POST /v1/chat/completions"`). Only the specified
method is handled; other methods return 405 unless a handler is also registered.

- Multiple calls with the same path replace the previous handler.
- The path handler takes precedence over the default `/cassette/<id>/v1/messages`
  dispatch for that path.
- The handler receives a standard `http.HandlerFunc` -- no custom `HandlerFunc`
  type, no `cassetteID` parameter injected.
- Returns `*MockServer` for method chaining.

**Example (OpenAI SSE):**
```go
ms.SetPathHandler("POST /v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
    w.Header().Set("Content-Type", "text/event-stream")
    w.WriteHeader(http.StatusOK)
    w.Write([]byte(sseContent))
})
```

**Example (OpenAI JSON):**
```go
ms.SetPathHandler("POST /v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(http.StatusOK)
    w.Write([]byte(jsonContent))
})
```

## API: internal/testutil/mockapi -- Types

### APIRequest

```go
type APIRequest struct {
    Body   map[string]any  // decoded JSON request body
    Header http.Header      // cloned request headers
}
```

`APIRequest` represents one captured request received by the mock server.
`Body` is the decoded JSON request body. `Header` is a clone of the HTTP request
headers.

Defined in `internal/testutil/mockapi/server.go:16-19`.

### MockBehavior

```go
type MockBehavior struct {
    RejectEmptyToolProperties bool
}
```

`MockBehavior` defines custom mock behaviors. Currently the only option is
`RejectEmptyToolProperties`, which causes the server to return HTTP 400 if a
request includes a tool definition with an empty `input_schema.properties` map.

Defined in `internal/testutil/mockapi/server.go:36-38`.

### MockServer.Server (public field)

```go
type MockServer struct {
    Server       *httptest.Server  // public; tests and harness may access directly
    CassetteDir  string
    ...
}
```

The `Server` field is a public `*httptest.Server`. Tests and the e2e harness may
access it directly. The plan does not require encapsulating it.

## Usage

### Using NewTestServer (recommended)

`NewTestServer` is a convenience helper that combines server creation, environment
setup, and cleanup registration in one call:

```go
func NewTestServer(t *testing.T, cassetteID string, opts ...Option) *MockServer
```

`NewTestServer` performs the following steps:
1. Calls `Lookup(cassetteID)` to resolve the cassette file path. Panics if not found.
2. Calls `NewMockServer(WithCassetteDir(cassetteDir))` where `cassetteDir` is the
   directory portion of the resolved path.
3. Calls `t.Setenv("ANTHROPIC_BASE_URL", ms.URL())` and
   `t.Setenv("OPENAI_BASE_URL", ms.URL())` -- both environment variables are set.
4. Calls `t.Cleanup(ms.Close)` to register automatic cleanup.
5. Applies all `opts` in order.
6. Returns `ms`.

**Example:**
```go
// Replace boilerplate:
//   server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { ... }))
//   defer server.Close()
//   t.Setenv("ANTHROPIC_BASE_URL", server.URL)
//
// With:
server := mockapi.NewTestServer(t, "anthropic/hello-world")
// ANTHROPIC_BASE_URL and OPENAI_BASE_URL already set; cleanup already registered
```

### Using MockServer Directly

```go
server := mockapi.NewMockServer(mockapi.WithCassetteDir("internal/testutil/mockapi/testdata"))
defer server.Close()

// Use server.URL() as the base URL for the API client
client := api.NewClient(server.URL())
```

### Content Inspection

After a request is made, use `ms.Requests() []APIRequest` to inspect captured
requests and verify their fields:

```go
ms := mockapi.NewMockServer(opts...)
// ... make requests via the client ...

reqs := ms.Requests()
if len(reqs) == 0 {
    t.Fatal("expected at least one request")
}
// Verify the request body and headers
req := reqs[0]
if model, ok := req.Body["model"].(string); !ok || model == "" {
    t.Error("expected non-empty model in request body")
}
if auth := req.Header.Get("Authorization"); auth == "" {
    t.Error("expected Authorization header")
}
```

## Limitations

`httptest.NewServer` remains the correct tool for the following test scenarios;
mockapi cannot serve them:

1. **`internal/tool/web_fetch_blackbox_test.go`** (6 calls): Mocks arbitrary HTTP
   content -- HTML, PNG, 302 redirects, 11 MB bodies. These test the web-fetch
   tool against non-API endpoints with completely different content types.

2. **`internal/agent/engine_thinking_test.go:417`** (1 call): Uses `http.Hijacker`
   to simulate a broken stream mid-response. The AC6 fallback path requires raw
   connection control that cannot be expressed as a static cassette.

The permanent exceptions are fully documented in `mockapi-migration.md` under
"Permanent Exceptions".

## Cassette File Formats

### .sse Format (SSE/streaming)

Plain text. One or more SSE events, each in the form:

```
event: <event-type>
data: <JSON>

```

The file must end with a blank line (`\n\n`). Without it, the last event may not
be flushed by the SSE handler.

**Example:**
```
event: message_start
data: {"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","content":[],"model":"test","usage":{"input_tokens":1,"output_tokens":1}}}

event: content_block_stop
data: {"type":"content_block_stop","index":0}

event: message_stop
data: {"type":"message_stop"}

```

### .json Format (non-streaming)

A single valid JSON document. Whitespace is permitted; the loader trims it before
serving. The JSON is written directly to the HTTP response body without SSE framing.

**Example:**
```json
{
  "id": "chatcmpl-123",
  "object": "chat.completion",
  "created": 1677652288,
  "model": "gpt-5.4-nano",
  "choices": [{
    "index": 0,
    "message": {
      "role": "assistant",
      "content": "Hello! How can I help you today?"
    },
    "finish_reason": "stop"
  }],
  "usage": {
    "prompt_tokens": 10,
    "completion_tokens": 15,
    "total_tokens": 25
  }
}
```

### Cassette Directory Layout

Cassettes are stored under `internal/testutil/mockapi/testdata/` with a two-level
provider-prefixed structure:

```
internal/testutil/mockapi/testdata/
├── anthropic/
│   ├── hello-world.sse
│   ├── message-stream-1.sse
│   └── ...
└── openai/
    ├── chat-basic.json
    ├── chat-stream.sse
    └── ...
```

Naming convention: `{provider}/{cassette-id}.{sse|json}`

- `provider`: `anthropic` or `openai` -- lowercase
- `cassette-id`: lowercase, hyphen-separated, descriptive
- No version numbers in filenames unless needed to disambiguate