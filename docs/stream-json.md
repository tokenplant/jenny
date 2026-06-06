---
title: Stream-JSON Output Protocol
slug: stream-json
priority: P0
status: partial
spec: complete
code: partial
package: internal/cli, internal/agent
gaps:
  - No stdout guard
  - Missing system/init line
  - Flat tool_use uses tool_input not parameters
  - No tool_call started/completed
  - Simplified event shape vs SDK
depends_on:
  - cli
  - agent-loop
  - sse-streaming
---
# Stream-JSON Output Protocol

## Overview

Headless Jenny runs emit **NDJSON** (newline-delimited JSON) on **stdout only**. Each line is one JSON object. Debug and logs go to **stderr**. This protocol must be fully compatible with SDK consumers that parse agent activity.

## Requirements

- Requires non-interactive mode (`-p` / `--print` or positional prompt).
- Install stdout guard when `--output-format stream-json` to prevent non-JSON leakage to stdout.
- JSON stringify must escape U+2028/U+2029 for NDJSON safety.

## Message Sequence (Typical Turn)

```
1. system/init          (once per process start or resume)
2. assistant            (model response, may include tool_use in message)
3. tool_progress        (optional, during tool execution)
4. user                 (tool_result blocks)
5. assistant            (next model turn)
…
N. result               (terminal line, always last on success)
```

## Message Types

### `system` / `init`

First line after startup:

```json
{
  "type": "system",
  "subtype": "init",
  "cwd": "/path/to/project",
  "session_id": "sess_…",
  "tools": ["Read", "Write", "Bash", …],
  "mcp_servers": [{ "name": "…", "status": "connected" }],
  "model": "deepseek-v4-flash",
  "permissionMode": "default",
  "uuid": "…"
}
```

Optional fields: `slash_commands`, `skills`, `plugins`.

### `assistant`

Wraps API-shaped assistant message:

```json
{
  "type": "assistant",
  "message": {
    "role": "assistant",
    "content": [
      { "type": "text", "text": "…" },
      { "type": "tool_use", "id": "toolu_…", "name": "Read", "input": { "file_path": "…" } }
    ]
  },
  "session_id": "sess_…",
  "uuid": "…"
}
```

### Flat `tool_use` (legacy headless parsers)

When emitting flat tool events (not full assistant wrapper), use **`parameters`**, not `tool_input`:

```json
{
  "type": "tool_use",
  "tool_name": "Read",
  "parameters": { "file_path": "foo.go" },
  "session_id": "sess_…"
}
```

### `user` (tool results)

```json
{
  "type": "user",
  "message": {
    "role": "user",
    "content": [
      {
        "type": "tool_result",
        "tool_use_id": "toolu_…",
        "content": "…",
        "is_error": false
      }
    ]
  },
  "session_id": "sess_…"
}
```

### `tool_call` started / completed

Headless activity parsers may receive:

```json
{ "type": "tool_call", "subtype": "started", "tool_name": "Bash", "tool_use_id": "…" }
{ "type": "tool_call", "subtype": "completed", "tool_use_id": "…", "is_error": false }
```

### `stream_event` (partial messages)

When `--include-partial-messages` is set, forward raw SSE events:

```json
{
  "type": "stream_event",
  "event": { "type": "content_block_delta", "delta": { "type": "text_delta", "text": "Hel" } }
}
```

Requires live SSE streaming from API (see [`sse-streaming.md`](./sse-streaming.md)).

### `system` / `compact_boundary`

Emitted after context compaction:

```json
{
  "type": "system",
  "subtype": "compact_boundary",
  "compact_metadata": {
    "trigger": "auto",
    "pre_tokens": 180000,
    "preserved_segment": "…"
  }
}
```

### `result` (terminal)

Always the last line on successful run:

```json
{
  "type": "result",
  "subtype": "success",
  "result": "Final assistant text",
  "session_id": "sess_…",
  "usage": {
    "input_tokens": 100,
    "output_tokens": 50,
    "cache_read_input_tokens": 0,
    "cache_creation_input_tokens": 0
  },
  "total_cost_usd": 0.001,
  "duration_ms": 3000,
  "num_turns": 2,
  "stop_reason": "end_turn"
}
```

Error subtypes: `error`, `error_max_turns`, `error_budget`, etc.

## Edge Cases

| Case | Expected behavior |
|------|-------------------|
| Log/debug output | stderr only; never stdout in stream-json mode |
| Empty final text | `result` with empty string, still emit usage |
| Tool error | `is_error: true` on tool_result; still continue to result |
| Interrupt | Synthetic error tool_results for pending tool_use |
| Resume | Same `session_id` in all lines |

## Acceptance Criteria

- **AC1:** Every stdout line valid JSON when format is stream-json.
- **AC2:** Flat tool_use events use `parameters` key.
- **AC3:** Terminal line is always `type: result` with usage snake_case fields.
- **AC4:** `session_id` consistent across init, turns, and result.
- **AC5:** Partial events only when `--include-partial-messages` and SSE enabled.

## Related

- CLI flags: [`cli.md`](./cli.md)
- Cost fields: [`cost-tracking.md`](./cost-tracking.md)
- SSE dependency: [`sse-streaming.md`](./sse-streaming.md)
