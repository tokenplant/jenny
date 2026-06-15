---
title: Stream-JSON Output Protocol
slug: stream-json
priority: P0
status: done
spec: complete
code: done
defer_to: P3
package: internal/cli, internal/agent
gaps:
  - "kind" field emitted on every event — Claude Code SDK schema does not include this
  - "tool_call started/completed" format — Claude Code uses "tool_progress" instead
  - "stream_request_start" emitted before each API iteration — Claude Code discards this internally
  - jenny emits one "assistant" per turn (aggregated); Claude Code emits one per content block with shared message.id
  - Missing `system/subtype: thinking_tokens` events emitted during thinking blocks
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

## Common Fields

Every event carries these fields in declaration order:

| Field | Type | Description |
|-------|------|-------------|
| `type` | string | Event type identifier |
| `session_id` | string | Stable session identifier |
| `parent_tool_use_id` | string | Parent tool use ID for nested events; omitted when nil |
| `uuid` | string | Unique event identifier |

## Message Sequence (Typical Turn)

```
1. system/init          (once per process start or resume)
2. stream_request_start (before each API iteration; jenny extension)
3. stream_event        (raw SSE deltas when --include-partial-messages)
4. assistant           (aggregated final message after content_block_stop)
5. tool_call/started   (before tool execution)
6. tool_call/completed (after tool execution)
7. user (aggregated tool result batch after last tool_call completed)
8. [repeat 2-7 for each turn]
…
N. result              (terminal line, always last)
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
  "mcp_servers": [],
  "model": "MiniMax-M2.7",
  "permissionMode": "default",
  "fast_mode_state": "off",
  "output_style": "default",
  "claude_code_version": "2.1.172",
  "apiKeySource": "none",
  "analytics_disabled": true,
  "product_feedback_disabled": true,
  "uuid": "…",
  "memory_paths": { "auto": "/path/to/memory/" },
  "agents": ["claude", "Explore", …],
  "slash_commands": ["adapt", "audit", …],
  "skills": ["adapt", "audit", …],
  "plugins": [{ "name": "gopls-lsp", "path": "…", "source": "…" }]
}
```

Required fields:
- `cwd`, `session_id`, `tools`, `mcp_servers`, `model`, `permissionMode`, `fast_mode_state`, `output_style`, `claude_code_version`, `uuid`

Common optional fields:
- `apiKeySource`: Source of API key (`"none"` for no API key)
- `analytics_disabled`, `product_feedback_disabled`: Boolean flags
- `memory_paths`: Object with `auto` path for session memory directory
- `agents`: Array of available agent names
- `slash_commands`, `skills`, `plugins`: Feature enablement lists

### `stream_request_start`

Emitted before each API iteration. **This is a jenny extension** — not part of the headless-agent reference format.

```json
{
  "type": "stream_request_start",
  "session_id": "sess_…",
  "parent_tool_use_id": null,
  "uuid": "…"
}
```

### `kind` Field

All events include a `kind` field for Claude Code parser compatibility. **jenny extension** — Claude Code does not emit this field.

```json
{
  "type": "assistant",
  "kind": "assistant",
  "message": { … },
  "session_id": "sess_…",
  "uuid": "…"
}
```

The `kind` value matches the event `type` (e.g., `"assistant"`, `"user"`, `"result"`).

### `usage` Field on `assistant` Messages

The `assistant` message includes a `usage` field in its `message` sub-object, populated from per-turn `message_start` / `message_delta` stream event accumulation:

```json
{
  "type": "assistant",
  "message": {
    "id": "msg_…",
    "type": "message",
    "role": "assistant",
    "model": "deepseek-v4-flash",
    "content": [ … ],
    "stop_reason": null,
    "stop_sequence": null,
    "usage": { "input_tokens": 100, "cache_creation_input_tokens": 0, "cache_read_input_tokens": 0, "output_tokens": 50, "service_tier": "standard" }
  },
  …
}
```

### `assistant` (Aggregated)

Emitted **once per content block** (thinking, text, or tool_use), not once per turn. Multiple `assistant` events that share the same `message.id` belong to the same API turn. Field order: `type`, `message`, `parent_tool_use_id`, `session_id`, `uuid`:

```json
{
  "type": "assistant",
  "message": {
    "id": "msg_…",
    "type": "message",
    "role": "assistant",
    "model": "deepseek-v4-flash",
    "content": [
      { "type": "thinking", "thinking": "…", "signature": "…" },
      { "type": "text", "text": "…" },
      { "type": "tool_use", "id": "toolu_…", "name": "Read", "input": { "file_path": "…" } }
    ],
    "stop_reason": null,
    "stop_sequence": null,
    "usage": { "input_tokens": 100, "cache_creation_input_tokens": 0, "cache_read_input_tokens": 0, "output_tokens": 50, "service_tier": "standard" }
  },
  "parent_tool_use_id": null,
  "session_id": "sess_…",
  "uuid": "…"
}
```

Note: `parent_tool_use_id` comes before `session_id` in field order. `stop_reason` and `stop_sequence` are always present (null when not set). The `usage` field is populated from per-turn stream event accumulation (see `usage` field on `assistant` Messages above).

### `user` (Aggregated Tool Results)

Emitted after tool execution completes, before the next API iteration. Includes `timestamp` (ISO-8601) and `tool_use_result`:

```json
{
  "type": "user",
  "message": {
    "role": "user",
    "content": [
      {
        "type": "tool_result",
        "tool_use_id": "call_…",
        "content": "tool output text",
        "is_error": false
      }
    ]
  },
  "parent_tool_use_id": null,
  "session_id": "sess_…",
  "uuid": "…",
  "timestamp": "2026-06-09T13:21:29.644Z",
  "tool_use_result": { "type": "text", "file": { "filePath": "…", "content": "…", "numLines": 10, "startLine": 1, "totalLines": 10 } }
}
```

`tool_use_result` format:
- **File read success**: `object` with `type: "text"` and `file` metadata (filePath, content, numLines, startLine, totalLines)
- **Error/permission denied**: `string` containing the error message. The `content` array's `tool_result` block has `is_error: true`

### `tool_progress` (Claude Code uses `tool_progress`, not `tool_call`)

Claude Code emits `tool_progress` for long-running tool progress updates. jenny emits `tool_call started/completed` instead — this is a jenny-specific format.

```json
// jenny-only (not in Claude Code SDK schema)
{ "type": "tool_call", "subtype": "started", "tool_name": "Bash", "tool_use_id": "…", "session_id": "sess_…", "parent_tool_use_id": null, "uuid": "…" }
{ "type": "tool_call", "subtype": "completed", "tool_use_id": "…", "is_error": false, "session_id": "sess_…", "parent_tool_use_id": null, "uuid": "…" }
```

### `stream_request_start` (jenny extension)

Emitted before each API iteration. **jenny extension** — Claude Code QueryEngine discards this internally and does not emit it to SDK consumers.

```json
{ "type": "stream_request_start", "session_id": "sess_…", "parent_tool_use_id": null, "uuid": "…" }
```

### `stream_event` (partial messages)

When `--include-partial-messages` is set, forward raw SSE events. Inner event objects emit only type-relevant fields (no zero-value Go struct padding). The `event` object's `type` field is always first.

```json
{
  "type": "stream_event",
  "parent_tool_use_id": null,
  "session_id": "sess_…",
  "uuid": "…",
  "event": { "type": "content_block_delta", "index": 1, "delta": { "type": "text_delta", "text": "Hel" } }
}
```

For `content_block_start`, inner `content_block` has only relevant fields (e.g., `type`, `thinking`, `signature` for thinking blocks; `type`, `id`, `name`, `input` for tool_use).

For `message_delta`, inner `delta` has only `stop_reason` and `stop_sequence` (no `container` or `stop_details`).

For `message_start`, the `message` object always includes `stop_reason` and `stop_sequence` fields (null when not yet set).

For `tool_progress`, inner event contains tool progress for long-running operations.

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

### `result` (Terminal)

Always the last line on successful run. Note: `parent_tool_use_id` is NOT present in result events (this differs from other event types). Field order matches reference format:

```json
{
  "type": "result",
  "subtype": "success",
  "is_error": false,
  "api_error_status": null,
  "duration_ms": 3000,
  "duration_api_ms": 2800,
  "ttft_ms": 1203,
  "ttft_stream_ms": 1000,
  "time_to_request_ms": 50,
  "num_turns": 2,
  "result": "Final assistant text",
  "stop_reason": "end_turn",
  "session_id": "sess_…",
  "total_cost_usd": 0.001,
  "usage": {
    "input_tokens": 100,
    "output_tokens": 50,
    "cache_read_input_tokens": 0,
    "cache_creation_input_tokens": 0,
    "server_tool_use": { "web_search_requests": 0, "web_fetch_requests": 0 },
    "service_tier": "standard",
    "cache_creation": { "ephemeral_1h_input_tokens": 0, "ephemeral_5m_input_tokens": 0 },
    "inference_geo": "",
    "iterations": [],
    "speed": "standard"
  },
  "modelUsage": {
    "deepseek-v4-flash": {
      "inputTokens": 100,
      "outputTokens": 50,
      "cacheReadInputTokens": 0,
      "cacheCreationInputTokens": 0,
      "webSearchRequests": 0,
      "contextWindow": 200000,
      "maxOutputTokens": 32000
    }
  },
  "terminal_reason": "completed",
  "permission_denials": [],
  "fast_mode_state": "off",
  "uuid": "…"
}
```

Field order: `type`, `subtype`, `is_error`, `api_error_status`, `duration_ms`, `duration_api_ms`, `ttft_ms`, `ttft_stream_ms`, `time_to_request_ms`, `num_turns`, `result`, `stop_reason`, `session_id`, `total_cost_usd`, `usage`, `modelUsage`, `terminal_reason`, `permission_denials`, `fast_mode_state`, `uuid`.

New result event fields:
- `ttft_ms`: Time to first token in milliseconds. Measured from API call start to first content block received. Omitted if zero.
- `ttft_stream_ms`: Time from API call start to first token received on the stream. Omitted if zero.
- `time_to_request_ms`: Time to build and send the API request. Omitted if zero.
- `terminal_reason`: Maps the stop reason to a stable string. `"completed"` for end_turn/stop_sequence, `"max_tokens"` for max_tokens. Omitted if empty.
- `api_error_status`: `null` on successful API response. Contains the error message string when the API call fails permanently (after retry exhaustion). Always present.

Error subtypes: `error`, `error_max_tokens`, `error_max_turns`, `error_budget`.

#### `error_max_tokens` shape

When output is capped due to `max_tokens`, the `result` event includes `error_max_tokens`:

```json
{
  "type": "result",
  "subtype": "error_max_tokens",
  "result": "max tokens reached: output_cap",
  "session_id": "sess_…",
  "parent_tool_use_id": null,
  "uuid": "…",
  "usage": { "input_tokens": 100, "output_tokens": 50, "cache_read_input_tokens": 0, "cache_creation_input_tokens": 0 },
  "total_cost_usd": 0.001,
  "duration_ms": 3000,
  "duration_api_ms": 2800,
  "num_turns": 2,
  "stop_reason": "max_tokens",
  "error_max_tokens": {
    "category": "output_cap",
    "output_tokens": 50,
    "max_output_tokens": 50,
    "input_tokens": 100,
    "threshold": 150000
  },
  "modelUsage": { … }
}
```

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
- **AC6:** `total_cost_usd` appears exactly once — on the terminal `result` event.
- **AC7:** `parent_tool_use_id` is present when non-nil; omitted when nil (top-level events).
- **AC8:** Field order matches reference format: `type`, then `event|message|payload`, then `session_id`, `parent_tool_use_id`, `uuid`, then remaining fields.

## Related

- CLI flags: [`cli.md`](./cli.md)
- Cost fields: [`cost-tracking.md`](./cost-tracking.md)
- SSE dependency: [`sse-streaming.md`](./sse-streaming.md)
