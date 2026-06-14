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
  []
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
  "model": "deepseek-v4-flash",
  "permissionMode": "default",
  "fast_mode_state": "off",
  "output_style": "default",
  "claude_code_version": "1.0.0",
  "uuid": "…"
}
```

Required fields for Claude Code compatibility:
- `fast_mode_state`: Always `"off"` for regular mode. Signals whether the session uses fast mode.
- `output_style`: Always `"default"`. Signals output formatting preferences.
- `mcp_servers`: Array of MCP server names (strings). Empty `[]` if no MCP servers configured.

Optional fields: `slash_commands`, `skills`, `plugins`.

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

### `assistant` (Aggregated)

Wraps the complete API-shaped assistant message after `content_block_stop`. Emitted **once per turn**, after all content blocks are received. Field order: `type`, `message`, `parent_tool_use_id`, `session_id`, `uuid`:

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

Note: `parent_tool_use_id` comes before `session_id` in field order. `stop_reason` and `stop_sequence` are always present (null when not set).

### `user` (Aggregated Tool Results)

Emitted after the last `tool_call` `completed` event in a batch, before the next `stream_request_start`. Includes `timestamp` (ISO-8601) and `tool_use_result`:

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
  "parent_tool_use_id": null,
  "session_id": "sess_…",
  "uuid": "…",
  "timestamp": "2026-06-09T13:21:29.644Z",
  "tool_use_result": { "stdout": "…", "stderr": "", "interrupted": false, "isImage": false, "noOutputExpected": false }
}
```

For errors, `tool_use_result` is a string: `"Error: …"`

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

### `tool_call` started / completed

```json
{ "type": "tool_call", "subtype": "started", "tool_name": "Bash", "tool_use_id": "…", "session_id": "sess_…", "parent_tool_use_id": null, "uuid": "…" }
{ "type": "tool_call", "subtype": "completed", "tool_use_id": "…", "is_error": false, "session_id": "sess_…", "parent_tool_use_id": null, "uuid": "…" }
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
  "duration_ms": 3000,
  "duration_api_ms": 2800,
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
  "permission_denials": [],
  "fast_mode_state": "off",
  "uuid": "…"
}
```

Field order: `type`, `subtype`, `is_error`, `duration_ms`, `duration_api_ms`, `num_turns`, `result`, `stop_reason`, `session_id`, `total_cost_usd`, `usage`, `modelUsage`, `permission_denials`, `fast_mode_state`, `uuid`.

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
