# Claude-Code-compatible stream-json Format Specification

This document defines the NDJSON (Newline Delimited JSON) streaming format used by Claude Code (`--output-format=stream-json`). This is the format that `jenny` must adhere to for compatibility with SDK callers and the Claude CLI ecosystem.

## 1. General Principles
- **NDJSON**: Each line on `stdout` must be a single, valid JSON object followed by a newline (`\n`).
- **Envelopes**: Every message is an "Envelope" containing metadata and a `type`-specific payload.
- **Anthropic Compatibility**: Message content (User/Assistant) follows the [Anthropic Messages API](https://docs.anthropic.com/en/api/messages) structure.
- **Liveness**: `stream_event` messages provide real-time feedback; `assistant`/`user` messages provide the final "frozen" state of a turn.

---

## 2. Common Metadata (The Envelope)
Almost every message includes these fields:

| Field | Type | Description |
| :--- | :--- | :--- |
| `type` | string | The top-level event category (e.g., `user`, `assistant`, `stream_event`, `system`, `result`, `control_request`, `control_response`). |
| `session_id` | string (UUID) | Unique ID for the conversation session. |
| `uuid` | string (UUID) | Unique ID for this specific event/line. |
| `parent_tool_use_id` | string \| null | (Optional) The ID of the tool call that triggered this sequence of messages. |

---

## 3. Core Message Types

### 3.1 User Message (`type: "user"`)
Emitted when the user provides input or a **tool returns a result**. Tool results are nested blocks.

```json
{
  "type": "user",
  "session_id": "...",
  "uuid": "...",
  "message": {
    "role": "user",
    "content": [
      {
        "type": "tool_result",
        "tool_use_id": "call_01",
        "content": "Output from the tool..."
      }
    ]
  }
}
```

### 3.2 Assistant Message (`type: "assistant"`)
Emitted when a content block completes. Multiple `assistant` events may share the same `message.id` — one per content block in the turn.

**One event per content block**: Claude Code emits one `assistant` per content block (thinking, text, or tool_use), not one per turn. Content blocks that share the same `message.id` belong to the same API turn. Implementations MUST emit one `assistant` event per content_block_stop.

**`usage` field**: The `message` sub-object includes a `usage` field with accumulated token counts for this turn (input tokens, output tokens, cache tokens).

**Content block ordering rule**: Thinking blocks appear before text blocks in `message.content` and MUST NOT be merged into the text block. A thinking block is always emitted as its own object with `type: "thinking"`.

**Signature field rule**: When the API returns a thinking block with a non-empty `signature`, the emitted block MUST include `"signature": "<value>"`. When `signature` is empty/absent, the `"signature"` key MUST be omitted (omitempty).

**Correct pattern (one event):**
```json
{
  "type": "assistant",
  "session_id": "...",
  "uuid": "...",
  "message": {
    "role": "assistant",
    "content": [
      { "type": "thinking", "thinking": "Let me look at the files...", "signature": "abc123" },
      { "type": "text", "text": "I've analyzed the logs." },
      { "type": "tool_use", "id": "call_02", "name": "ls", "input": { "path": "src/" } }
    ]
  }
}
```

A second correct example showing the text-only and tool-only variants (each still one `assistant` event per turn):
```json
{
  "type": "assistant",
  "session_id": "...",
  "uuid": "...",
  "message": {
    "role": "assistant",
    "content": [
      { "type": "text", "text": "Hello" },
      { "type": "tool_use", "id": "t1", "name": "Read", "input": { "file_path": "foo" } },
      { "type": "tool_use", "id": "t2", "name": "Bash", "input": { "command": "ls" } }
    ]
  }
}
```

**Incorrect pattern (duplication — do not use):** Emitting one `assistant` per `tool_use` causes text to be repeated across events when a turn contains text followed by multiple tool calls. The above turn must produce exactly ONE `assistant` line.

### 3.3 Streaming Event (`type: "stream_event"`)
Emitted for incremental updates when `--include-partial-messages` is set. `event` field contains a standard Anthropic stream event.

```json
{
  "type": "stream_event",
  "session_id": "...",
  "uuid": "...",
  "event": {
    "type": "content_block_delta",
    "index": 0,
    "delta": { "type": "thinking_delta", "thinking": "Checking the " }
  }
}
```

### 3.4 Turn Result (`type: "result"`)
The summary event emitted at the end of a query turn.

```json
{
  "type": "result",
  "subtype": "success",
  "result": "Final human-readable summary",
  "duration_ms": 1500,
  "duration_api_ms": 1200,
  "total_cost_usd": 0.0045,
  "usage": {
    "input_tokens": 1200,
    "output_tokens": 300,
    "cache_read_input_tokens": 0,
    "cache_creation_input_tokens": 0
  },
  "modelUsage": {
    "claude-3-7-sonnet": { "inputTokens": 1200, "outputTokens": 300, "costUSD": 0.0045 }
  },
  "stop_reason": "end_turn",
  "session_id": "...",
  "uuid": "..."
}
```

---

## 4. Control Protocol (Interactive)

### 4.1 Control Request (`type: "control_request"`)
The CLI requests something from the SDK/Host (e.g., a permission decision).

```json
{
  "type": "control_request",
  "request_id": "req_uuid_123",
  "request": {
    "subtype": "can_use_tool",
    "tool_name": "Bash",
    "input": { "command": "rm -rf /" },
    "tool_use_id": "call_99",
    "description": "Delete everything"
  }
}
```

### 4.2 Control Response (`type: "control_response"`)
The SDK/Host responds to a request.

```json
{
  "type": "control_response",
  "request_id": "req_uuid_123",
  "response": {
    "decision": "allow",
    "reason": "User clicked 'Allow'"
  }
}
```

---

## 5. System Messages (`type: "system"`)

### 5.1 Initialization (`subtype: "init"`)
The very first message emitted. Includes environment context.
```json
{
  "type": "system",
  "subtype": "init",
  "claude_code_version": "2.1.172",
  "cwd": "/Users/user/project",
  "tools": ["Bash", "Edit", "Read", "Grep"],
  "model": "claude-3-7-sonnet-20250219",
  "permissionMode": "default",
  "fast_mode_state": "off",
  "output_style": "default",
  "session_id": "...",
  "uuid": "..."
}
```

### 5.2 Status & State
- `subtype: "status"`: Emitted for `permissionMode` changes.
- `subtype: "session_state_changed"`: Emitted with `state: "idle"` or `state: "busy"`.
- `subtype: "task_started"`: Emitted when a background task (like a subagent) begins.
- `subtype: "thinking_tokens"`: Emitted during extended thinking. Each event carries `estimated_tokens` (running total) and `estimated_tokens_delta` (increment since last event).

---

## 6. Comparison: `jenny` vs Official Spec

| Feature | `jenny` | Official Spec |
| :--- | :--- | :--- |
| **Tool Results** | ✅ Wrapped in user message | ✅ Wrapped in user message |
| **Thinking** | ✅ stream_event or assistant blocks | ✅ stream_event or assistant blocks |
| **IDs** | ✅ session_id + uuid on every line | ✅ session_id + uuid on every line |
| **Tool Inputs** | ✅ input inside tool_use | ✅ input inside tool_use |
| **`usage` on `assistant`** | ✅ Included | ✅ Included |
| **One `assistant` per content block** | ❌ jenny emits one per turn | Spec requires one per turn; Claude Code does one per block |
| **`kind` field** | ❌ jenny extension | Not present |
| **`tool_call` started/completed** | ❌ jenny uses tool_call | Spec uses `tool_progress` |
| **`thinking_tokens` system events** | ❌ Not present | Claude Code emits `system/subtype: thinking_tokens` during thinking |
| **`stream_request_start`** | ❌ jenny extension | Not emitted to SDK consumers |

## 7. Implementation Guide for `jenny`
1. **Refactor Envelope**: Ensure every `WriteStreamJSON` call injects a valid `session_id` and a fresh `uuid`.
2. **Standardize Role Messages**: Create helper functions to wrap tool results in User messages and format Assistant messages as an array of ContentBlocks.
3. **Handle Deltas**: Update the streaming loop to emit standard `stream_event` objects for thinking and text deltas.
4. **Implement Control Requests**: `jenny` must be able to emit `can_use_tool` requests if it's acting as a server, and handle `control_response` as input.

## Acceptance Criteria

- **AC-result-1:** The `result` event includes a `usage` object with `input_tokens` (integer ≥ 0) and `output_tokens` (integer ≥ 0).
- **AC-result-2:** The `result` event includes `total_cost_usd` (float ≥ 0.0). The field must be present even when the value is 0.
- **AC-result-3:** `duration_ms` is a non-negative number present on every `result` event.

## See Also

- [`stream-json.md`](./stream-json.md) — Detailed implementation guide and event type documentation
