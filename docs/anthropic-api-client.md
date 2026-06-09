---
title: Anthropic API Client
slug: anthropic-api-client
priority: P0
status: done
spec: complete
code: done
package: internal/api
gaps: []
defer_to: P3
depends_on:
  []
---
# Anthropic API Client

## Overview

Jenny's API client wraps the Anthropic Messages API for the agent loop. It handles message shape, system prompt placement, tool pairing, and media validation.

## System Prompt

System prompt is a **top-level request parameter**, not a `role: system` message in `messages[]`.

```json
{
  "model": "…",
  "system": [{ "type": "text", "text": "…" }],
  "messages": [ … ]
}
```

Multiple system blocks may be concatenated from prompt assembly (see [`system-prompt.md`](./system-prompt.md)).

## Tool Use / Tool Result Pairing

1. Assistant message with `tool_use` blocks must be sent **before** user message with matching `tool_result` blocks.
2. Each `tool_result.tool_use_id` must match a preceding `tool_use.id`.
3. On interrupt, synthesize error `tool_result` for every pending `tool_use`.

Normalization before send (see [`message-normalization.md`](./message-normalization.md)):

- Insert synthetic error results for missing IDs.
- Strip orphaned tool_results.
- Ensure assistant message includes full `tool_use` block (not just text).

## Thinking Blocks

Assistant messages with extended thinking:

- Thinking block must **not** be the last block in a message sent to API.
- Preserve thinking across assistant → tool_result → assistant turns.
- Strip trailing thinking from last assistant before request.

## Image and Media Validation

Pre-request validation:

| Limit | Value |
|-------|-------|
| Max media items per request | 100 |
| Max base64 size per image | 5 MB |

`validateImagesForAPI()` runs before send; fail fast with actionable error.

## Oversize Media Error Mapping

Map API 400/413 responses to user-facing strings:

| Condition | Message pattern |
|-----------|-----------------|
| Image too large (pre or post) | Actionable resize/remove guidance |
| Too many images dimensions | Compact or remove images guidance |
| PDF too many pages | Page limit guidance |
| Password-protected PDF | Password protected error |
| Invalid PDF | Invalid PDF error |
| Request too large (413) | Reduce context guidance |

After synthetic error, strip offending image/document blocks from meta user message on retry.

## Streaming vs Non-Streaming

Target: SSE streaming default with non-streaming fallback (see [`sse-streaming.md`](./sse-streaming.md)).

Jenny gap: non-streaming only today.

## Cache Headers

Prompt cache: optional cache control breakpoints on system blocks and stable prefixes. Track `cache_read_input_tokens` and `cache_creation_input_tokens` in usage.

The `anthropic-beta: prompt-caching-2024-07-31` header is sent on all requests. The tool definitions array is cached as a stable prefix by setting `cache_control` on the last tool entry.

## Edge Cases

| Case | Expected behavior |
|------|-------------------|
| Empty assistant after tool strip | Insert `[Tool use interrupted]` text |
| Bedrock consecutive user messages | Merge consecutive same-role messages |
| Invalid tool JSON in tool_use | Reject at parse or return tool error |
| Max output tokens exceeded | Retry with adjusted max_tokens (bounded) |

## Acceptance Criteria

- **AC1:** System prompt never sent as user/assistant message.
- **AC2:** Every tool_use has matching tool_result in following user message.
- **AC3:** Image count ≤ 100; each base64 ≤ 5 MB before send.
- **AC4:** Media errors map to specific user-facing strings.
- **AC5:** Trailing thinking stripped from last assistant block.

## Provider Compatibility

Tool serialization is provider-aware to maintain compatibility with alternate API providers that have different validation requirements.

### MiniMax Compatibility

When the provider is detected as MiniMax (see Detection below), tool serialization includes compatibility fixes for MiniMax error code 2013: "function name or parameters is empty".

#### Root Cause: web_search with WebSearchTool20250305Param

The primary issue was the `web_search` tool when serialized with `WebSearchTool20250305Param`. This SDK type has **no `input_schema` field at all**, causing MiniMax to reject it with error 2013.

```json
// WebSearchTool20250305Param (rejected by MiniMax - missing input_schema):
{"type": "web_search_20250305", "name": "web_search"}

// ToolParam with input_schema (accepted by MiniMax):
{"type": "tool", "name": "web_search", "input_schema": {"type": "object", "properties": {"query": {"type": "string"}}}}
```

**Fix:** For MiniMax provider, `web_search` is serialized as `ToolParam` with a standard `input_schema: {"type": "object", "properties": {"query": {"type": "string"}}}`, regardless of MaxUses. The `WebSearchTool20250305Param` path (which lacks `input_schema`) is only used for non-MiniMax providers.

#### Secondary Fix: __arg__ placeholder for empty properties

For tools with genuinely empty `properties`, a placeholder `__arg__` property is added when provider is MiniMax:

```json
// Before (rejected by MiniMax):
{"name": "empty_tool", "input_schema": {"type": "object", "properties": {}}}

// After (accepted by MiniMax):
{"name": "empty_tool", "input_schema": {"type": "object", "properties": {"__arg__": {"type": "string", "description": "Placeholder argument for empty schema"}}}}
```

Both fixes are provider-aware: they apply only when `ANTHROPIC_BASE_URL` contains "minimaxi". For non-MiniMax providers (e.g., the standard Anthropic endpoint), tool serialization is unchanged.

### DeepSeek Compatibility

DeepSeek enforces that each `tool_use` must have a single `tool_result`. When duplicate `tool_result` blocks with the same `tool_use_id` are present (e.g., from merging consecutive user messages), DeepSeek returns a 400 error:

```
messages.2.content.3: each tool_use must have a single result.
Found multiple `tool_result` blocks with id: call_01_...
```

**Fix:** Tool results are deduplicated by `tool_use_id` at two layers:

1. **Primary (normalize.go):** `mergeConsecutiveSameRole` deduplicates `ToolResults` when merging consecutive user messages, keeping the last occurrence (last-writer-wins).

2. **Safety net (client.go):** `deduplicateToolResults()` is called during SDK serialization as a defensive measure, ensuring no duplicate `tool_use_id` values reach the API regardless of how the message was constructed.

This fix is provider-agnostic and benefits all providers - DeepSeek is the primary beneficiary since it strictly enforces the uniqueness requirement.

### Detection

Provider detection is based on the `ANTHROPIC_BASE_URL` environment variable via `providerFromBaseURL()`. The function inspects the URL for known alternate provider substrings (currently `"minimaxi"`). If the URL contains a known substring, the MiniMax compatibility fix is applied during tool serialization in `toolToSDK()`. Otherwise, the standard Anthropic tool shape is used unchanged.

## Related

- Message normalization: [`message-normalization.md`](./message-normalization.md)
- Agent loop: [`agent-loop.md`](./agent-loop.md)
