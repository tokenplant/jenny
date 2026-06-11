---
title: mockapi Migration Guide
slug: mockapi-migration
priority: P3
status: done
spec: done
code: done (Stage 3)
package: internal/testutil/mockapi
depends_on: []
---
# mockapi Migration Guide

This document describes the mockapi extension and migration from `httptest.NewServer`
boilerplate to the centralized `mockapi` package. It is a time-bounded migration guide
written in Stage 2 and updated in Stage 7 to record outcomes.

## Overview

The mockapi package (`internal/testutil/mockapi`) was originally a minimal in-process
mock Anthropic API server that served SSE cassette files from a specified directory.
Each test that needed a mock API constructed its own `httptest.NewServer` with inline
handler logic, leading to duplicated boilerplate across `internal/api`,
`internal/agent`, and `e2e/harness`.

Stage 2 extends mockapi with five capabilities (E1-E5) to consolidate all API mocking
into a single package with a consistent interface. The `NewTestServer` helper further
eliminates boilerplate by combining server creation, environment variable setup, and
cleanup registration.

**Backward compatibility:** The old `NewMockServer(cassetteDir string)` API is removed.
Callers are updated in Stage 3. No shim is provided.

## The Five Extensions (E1-E5)

### E1 -- SetInlineResponse

```go
func (m *MockServer) SetInlineResponse(cassetteID string, content string) *MockServer
```

Stores SSE or JSON content in an in-memory map keyed by `cassetteID`. When a request
arrives for that `cassetteID`, the handler serves the inline content instead of reading
from disk.

**Why needed:** Many tests have small inline SSE fixtures. Writing them to temporary
files and managing cleanup is error-prone. With E1, the content lives directly in the
test code.

**Interaction with file-based cassettes:** The inline map is checked first. If no
inline content exists for the `cassetteID`, the handler falls back to the file-based
cassette from `WithCassetteDir`. This allows mixing inline and file-based content in
the same test.

### E2 -- SetRequestInspector

```go
func (m *MockServer) SetRequestInspector(fn func(r APIRequest) error) *MockServer
```

Registers a callback invoked after the request body is parsed and captured, before
the response is served. If the callback returns a non-nil error, the server responds
with HTTP 400 and `{"error": "<msg>"}`.

**Why needed:** Tests need to verify request headers (e.g., `Authorization`,
`Content-Type`) and request body fields (e.g., `model` is present) without maintaining
parallel state. E2 makes this a first-class assertion pattern.

**Example use case:**
```go
ms.SetRequestInspector(func(r APIRequest) error {
    if r.Header.Get("Authorization") == "" {
        return errors.New("missing Authorization header")
    }
    if model, ok := r.Body["model"].(string); !ok || model == "" {
        return errors.New("missing model in request body")
    }
    return nil
})
```

### E3 -- SetErrorResponse

```go
func (m *MockServer) SetErrorResponse(cassetteID string, statusCode int) *MockServer
```

Registers a non-200 HTTP status code to return for a given `cassetteID`. The error
response takes precedence over any cassette content.

**Why needed:** Tests for retry logic, context exhaustion, and BadGateway scenarios
require specific HTTP status codes. Without E3, these cannot be expressed using
cassette files alone.

**Confirmed use cases (verified in source during Stage 2 planning):**
- `retry_test.go:640-644` -- HTTP 429 rate limit response after 2 attempts
- `engine_test.go:2219` -- HTTP 400 with `prompt_too_long` error
- `engine_test.go:2408` -- HTTP 400 with MiniMax context limit error
- `loop_streaming_test.go:479` -- HTTP 502 BadGateway when `stream=true`

### E4 -- SetContentType

```go
func (m *MockServer) SetContentType(cassetteID string, contentType string) *MockServer
```

Overrides the `Content-Type` response header for a given `cassetteID`. The default
is `text/event-stream` for SSE cassettes.

**Why needed:** JSON responses from either provider use `application/json` as the
Content-Type. E4 lets the same cassette file be served with different content types
depending on the context.

**Use cases:**
- JSON responses (non-streaming) from either provider: `SetContentType(id, "application/json")`
- Plain text error responses: `SetContentType(id, "text/plain")`
- Clearing override: `SetContentType(id, "")` (resets to `text/event-stream`)

**Note:** For custom path handlers registered via `SetPathHandler`, the handler itself
writes the `Content-Type` header directly; `SetContentType` has no effect on those
paths.

### E5 -- SetPathHandler

```go
func (m *MockServer) SetPathHandler(pathPattern string, handler func(w http.ResponseWriter, r *http.Request)) *MockServer
```

Registers a custom `http.HandlerFunc` for a specific `METHOD /path` pattern. The
handler takes precedence over the default dispatcher for that path.

**Why needed:** OpenAI uses `/v1/chat/completions` instead of Anthropic's
`/v1/messages`. Some tests use custom paths for specialized routing. E5 allows
arbitrary path registration without changing the core mockapi dispatch logic.

**Path pattern format:** `METHOD /path` (e.g., `"POST /v1/chat/completions"`).
Only the specified method is handled; other methods return 405.

**Handler signature:** Standard `func(http.ResponseWriter, *http.Request)`. No custom
`HandlerFunc` type, no `cassetteID` parameter injected. The handler receives the
standard Go HTTP handler interface.

**Example -- OpenAI SSE:**
```go
ms.SetPathHandler("POST /v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
    w.Header().Set("Content-Type", "text/event-stream")
    w.WriteHeader(http.StatusOK)
    io.Copy(w, strings.NewReader(sseContent))
})
```

**Example -- OpenAI JSON:**
```go
ms.SetPathHandler("POST /v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(http.StatusOK)
    w.Write([]byte(jsonContent))
})
```

## NewTestServer Helper

```go
func NewTestServer(t *testing.T, cassetteID string, opts ...Option) *MockServer
```

`NewTestServer` is a convenience wrapper that eliminates the standard
`httptest.NewServer` boilerplate:

```go
// Before (boilerplate):
server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    // inline SSE handler ...
}))
defer server.Close()
t.Setenv("ANTHROPIC_BASE_URL", server.URL)
t.Setenv("OPENAI_BASE_URL", server.URL)

// After (NewTestServer):
server := mockapi.NewTestServer(t, "anthropic/hello-world")
// ANTHROPIC_BASE_URL and OPENAI_BASE_URL already set; cleanup already registered
```

**Behavior (5-step spec):**
1. Calls `Lookup(cassetteID)` to resolve the cassette file path. Panics with the
   error if the cassette is not found (invalid cassetteID is a test programming error).
2. Calls `NewMockServer(WithCassetteDir(cassetteDir))` where `cassetteDir` is the
   directory portion of the resolved path.
3. Calls `t.Setenv("ANTHROPIC_BASE_URL", ms.URL())` and
   `t.Setenv("OPENAI_BASE_URL", ms.URL())` -- both environment variables are set.
   This covers both Anthropic and OpenAI test scenarios without requiring callers to
   set env vars manually.
4. Calls `t.Cleanup(ms.Close)` to register automatic cleanup.
5. Applies all `opts` in order.
6. Returns `ms`.

**Why both ANTHROPIC_BASE_URL and OPENAI_BASE_URL:** OpenAI support is in scope
(Gap 5). Setting both allows a single helper call to work for either provider
without requiring callers to manually set the second env var. `t.Setenv` (not
`os.Setenv`) is used, so the variable is reset automatically at test cleanup.

**Lookup integration:** `NewTestServer` depends on the `Lookup` function to resolve
cassette IDs to filesystem paths. `Lookup` is defined in `internal/testutil/mockapi/`
and is implemented in Stage 4.

## Cassette Directory Layout

Cassettes are stored under `internal/testutil/mockapi/testdata/` with a flat
two-level provider-prefixed structure:

```
internal/testutil/mockapi/
├── server.go
├── server_test.go
├── lookup.go              # Lookup function (Stage 4)
└── testdata/
    ├── anthropic/
    │   ├── hello-world.sse
    │   ├── message-stream-1.sse
    │   ├── tool-use-turn1.sse
    │   └── partial-events.sse
    └── openai/
        ├── chat-basic.json        # JSON response (non-streaming)
        ├── chat-stream.sse        # OpenAI streaming SSE response
        └── chat-stream-reasoning.sse
```

**Naming convention:** `{provider}/{cassette-id}.{sse|json}`

- `provider`: `anthropic` or `openai` -- lowercase, no spaces
- `cassette-id`: lowercase, hyphen-separated, descriptive
- No version numbers in filenames unless needed to disambiguate

**Why flat two-level structure:** The provider prefix (`anthropic/`, `openai/`)
achieves necessary separation. Further subdirectory nesting (e.g.,
`anthropic/streaming/`) adds friction without benefit -- the cassette ID encodes
the distinction.

## Cassette File Formats

### .sse Format (streaming/SSE)

Plain text. One or more SSE events, each in the form:

```
event: <event-type>
data: <JSON>

```

The file must end with a blank line (`\n\n`). Without it, the last event may not
be flushed by the SSE handler.

**Constraints:**
- The JSON data portion must be a valid JSON value on a single `data:` line.
- Empty lines within the file are preserved as SSE line separators.
- File extension: `.sse`

**Example** (`server_test.go:15-16`):
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

**Constraints:**
- Must be a single valid JSON document (object or array).
- No SSE event boundaries. Raw JSON is written as `w.Write(cassetteData)`.
- File extension: `.json`

**Example** (`provider_openai_test.go:48-66`):
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

### Format Determination

| Format | Extension | Default Content-Type | Must end with |
|--------|-----------|---------------------|---------------|
| SSE (Anthropic) | `.sse` | `text/event-stream` | `\n\n` (double newline) |
| SSE (OpenAI) | `.sse` | `text/event-stream` | `\n\n` |
| JSON (non-streaming) | `.json` | `application/json` | `\n` (optional) |

The `.sse` and `.json` extensions are the primary determinant of Content-Type served
(unless `SetContentType` overrides per cassetteID). The handler does **not** distinguish
by HTTP path.

## OpenAI Support

OpenAI uses a different API path (`/v1/chat/completions`) and serves both JSON and
SSE response formats from the same endpoint. The original mockapi hard-coded
`POST /cassette/<id>/v1/messages` and `Content-Type: text/event-stream`.

OpenAI support requires E4 (SetContentType), E5 (SetPathHandler), and E1
(SetInlineResponse) working together:

**For OpenAI JSON (non-streaming) tests:**
1. Write a cassette file `openai/chat-basic.json` containing the JSON response.
2. Call `NewMockServer(WithCassetteDir("internal/testutil/mockapi/testdata"))`
3. Call `SetPathHandler("POST /v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
       // Read from file or use SetInlineResponse
       w.Header().Set("Content-Type", "application/json")
       w.WriteHeader(http.StatusOK)
       w.Write(cassetteData)
   })`
4. Call `SetContentType("openai/chat-basic", "application/json")` -- overrides the
   default Content-Type for the default dispatcher.

**For OpenAI SSE streaming tests:**
1. Write a cassette file `openai/chat-stream.sse` containing SSE events.
2. Call `SetPathHandler("POST /v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
       w.Header().Set("Content-Type", "text/event-stream")
       w.WriteHeader(http.StatusOK)
       io.Copy(w, strings.NewReader(sseContent))
   })`

**Header and body verification with SetRequestInspector:**
```go
ms.SetRequestInspector(func(r APIRequest) error {
    if r.Header.Get("Authorization") == "" {
        return errors.New("missing Authorization header")
    }
    if r.Header.Get("Content-Type") != "application/json" {
        return errors.New("expected Content-Type: application/json")
    }
    if model, ok := r.Body["model"].(string); !ok || model == "" {
        return errors.New("missing model in request body")
    }
    return nil
})
```

**Note on OpenAI SSE event names:** OpenAI SSE uses `data: {...}` lines without an
explicit `event:` line (implicit event). The implementer must verify the exact format
against the OpenAI SDK parser during Stage 6 migration. Read each call site directly
-- do not rely on prior line citations.

## Lookup Helper

```go
func Lookup(cassetteID string) (string, error)
```

`Lookup` resolves a cassette ID to an absolute filesystem path. It checks both `.sse`
and `.json` extensions and returns the path of the one that exists.

**cassetteID format:** `{provider}/{name}` (e.g., `anthropic/hello-world`).

**Examples:**
- `Lookup("anthropic/hello-world")` returns the path to `hello-world.sse` if it exists
- `Lookup("openai/chat-basic")` returns the path to `chat-basic.json` if it exists
- If both exist (should not happen), returns the `.sse` path

**Returns:** An error if neither `.sse` nor `.json` exists at the expected path.

## e2e/harness/mock_api.go

**Disposition:** Keep type aliases, delete `NewMockServer` delegation function, add
`NewTestServer` wrapper.

### Current state

```go
package harness

import "github.com/ipy/jenny/internal/testutil/mockapi"

type APIRequest = mockapi.APIRequest       // line 8
type MockServer = mockapi.MockServer        // line 11
type MockBehavior = mockapi.MockBehavior    // line 14

func NewMockServer(cassetteDir string) *MockServer { // line 17
    return mockapi.NewMockServer(cassetteDir) // line 18
}
```

### Why keep type aliases

The type aliases are used as a public contract in:
- `e2e/harness/types.go:74` -- `MockBehavior *MockBehavior`
- `e2e/harness/types.go:86` -- `APIRequests []APIRequestExpectation` (uses `APIRequest` in comparator)
- `e2e/harness/suite.go:15` -- `mockServer *MockServer`
- `e2e/harness/suite.go:101` -- `sr.mockServer.SetMockBehavior(tc.Target.MockBehavior)`
- `e2e/harness/suite.go:175` -- `sr.mockServer = NewMockServer(sr.Config.CassetteDir)`
- `e2e/harness/comparator.go:44` -- `compareAPIRequests(tc.Expected.APIRequests, actual.Requests)` -- note: `actual` is `[]RecordedRequest`, not `[]APIRequest`; the comparator inspects `RecordedRequest.Body` fields

Deleting `mock_api.go` entirely would require rewriting `types.go`, `suite.go`, and
`comparator.go` to import `mockapi` directly, which is more invasive than the benefit
justifies.

### Changes

**Delete:**
- `func NewMockServer(cassetteDir string) *MockServer` -- this delegation function is
  incompatible with the new `NewMockServer(opts ...Option)` signature. Deleted in Stage 3.

**Add:**
```go
// NewTestServer is a convenience wrapper around mockapi.NewTestServer.
// It delegates entirely to mockapi.NewTestServer.
func NewTestServer(t *testing.T, cassetteID string, opts ...mockapi.Option) *MockServer {
    return mockapi.NewTestServer(t, cassetteID, opts...)
}
```

### Final state (Stage 3)

```go
package harness

import "github.com/ipy/jenny/internal/testutil/mockapi"

// APIRequest aliases the mockapi type.
type APIRequest = mockapi.APIRequest

// MockServer aliases the mockapi type.
type MockServer = mockapi.MockServer

// MockBehavior aliases the mockapi type.
type MockBehavior = mockapi.MockBehavior

// NewTestServer is a convenience wrapper around mockapi.NewTestServer.
// It delegates entirely to mockapi.NewTestServer.
func NewTestServer(t *testing.T, cassetteID string, opts ...mockapi.Option) *MockServer {
    return mockapi.NewTestServer(t, cassetteID, opts...)
}
```

**Note:** `suite.go:175` calls `NewMockServer(sr.Config.CassetteDir)` with the old API.
In Stage 3, this call site is updated to use `NewTestServer(t, provider/cassette-id)` or
`NewMockServer(opts ...Option)`.

## Permanent Exceptions

The following `httptest.NewServer` call sites **cannot** be migrated to mockapi and
**must remain** as `httptest.NewServer` permanently:

### internal/tool/web_fetch_blackbox_test.go (6 calls)

**Reason:** These tests mock arbitrary HTTP content (HTML, PNG, 302 redirects, 11 MB
bodies). They test the web-fetch tool against completely different domain content
from API mocks. The web-fetch tool's purpose is to fetch arbitrary URLs; the test
server serves as a stand-in for those arbitrary URLs, not an API endpoint.

**What is tested:** The tool fetches arbitrary URLs (not a known API), so the mock
server cannot use a fixed cassette ID pattern. The content is arbitrary HTML/PNG/etc.,
not SSE or JSON API responses.

### internal/agent/engine_thinking_test.go:417 (1 call)

**Reason:** This test uses `http.Hijacker` to simulate a broken stream mid-response.
It tests the AC6 fallback path when streaming yields nothing but `streamResult.Blocks`
is populated. The hijack pattern is permanently incompatible with mockapi's
cassette-serving model because it requires controlling the connection at the raw HTTP
level, not just the response.

**What is tested:** The agent's fallback behavior when a streaming response fails
mid-stream. This requires injecting a partial SSE response followed by a connection
drop, which cannot be expressed as a static cassette.

### self-use in server.go:43

The `httptest.NewServer` call inside `internal/testutil/mockapi/server.go:43` is the
mockapi implementation itself and does not count as a test call site.

### Summary

| File | Line(s) | Count | Reason |
|------|---------|-------|--------|
| `internal/tool/web_fetch_blackbox_test.go` | various | 6 | Arbitrary HTTP content, completely different domain |
| `internal/agent/engine_thinking_test.go` | 417 | 1 | AC6 hijack pattern, permanently incompatible |
| `internal/testutil/mockapi/server.go` | 43 | 1 | Implementation self-use (not a test call site) |