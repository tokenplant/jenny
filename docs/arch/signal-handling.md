---
title: Signal Handling
slug: signal-handling
priority: P1
status: done
spec: complete
code: done
package: cmd/jenny
gaps: []
depends_on:
  - cli
---
# Signal Handling

## Overview

The jenny process must respond gracefully to SIGINT (Ctrl+C) and SIGTERM (kill) signals, allowing in-flight API calls to complete and session state to be flushed.

## Motivation

The `run()` function in `cmd/jenny/main.go` previously used `context.Background()` — a non-cancellable context — for the entire agent session. When the user pressed Ctrl+C, Go's default SIGINT handler killed the process without:
1. Waiting for in-flight API calls to complete
2. Flushing session state (cost tracking, transcripts)

## Implementation

**File: `cmd/jenny/main.go`**

Replace `context.Background()` with `signal.NotifyContext`:

```go
import (
    "context"
    "os/signal"    // NEW
    "syscall"     // NEW
    // ... existing imports
)

// Create context that cancels on Ctrl+C (SIGINT) or SIGTERM
ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
defer stop()
```

**How it works:**

1. `signal.NotifyContext` creates a context derived from `Background()` that cancels when the process receives `os.Interrupt` (Ctrl+C on all platforms) or `syscall.SIGTERM` (kill command on Unix).
2. When Ctrl+C is pressed, the context is cancelled.
3. The agent loop's top-of-iteration guard (`if ctx.Err() != nil`) at `internal/agent/engine_loop.go:160` catches this and returns gracefully.
4. Pending HTTP requests (which use `http.NewRequestWithContext(ctx, ...)`) are immediately aborted.
5. The `RunStream` return path flushes cost state and session transcript.

**Terminal output after Ctrl+C:**
```
Error: context canceled
```

## Edge Cases

| Scenario | Behavior |
|----------|----------|
| Ctrl+C during flag parsing | Go's default handler kills process immediately — acceptable (nothing to clean up) |
| Windows Ctrl+C | `os.Interrupt` is the equivalent — works correctly |
| Double Ctrl+C | Second SIGINT triggers Go's default behavior (immediate kill) — acceptable |
| SIGTERM (`kill <pid>`) | Context cancels, process exits cleanly |

## Portal Unchanged

The `jenny portal` command already has its own signal handler. This fix does not affect portal behavior.

## Acceptance Criteria

- **AC1:** Running `jenny -p "test"` and pressing Ctrl+C (SIGINT) cancels the context. The agent loop detects `ctx.Err()` and returns with `context.Canceled` error. The process exits cleanly within 1 second.
- **AC2:** Running `jenny -p "test"` and sending SIGTERM (`kill <pid>`) cancels the context. The process exits cleanly within 1 second.
- **AC3:** Running `jenny portal` and pressing Ctrl+C still works (portal already has its own signal handler — unchanged).
- **AC4:** `go test ./cmd/jenny/` passes. `go test ./internal/portal/` passes. No behavioral changes during normal (non-interrupted) execution.
