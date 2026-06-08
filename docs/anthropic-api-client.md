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

## Related

- Message normalization: [`message-normalization.md`](./message-normalization.md)
- Agent loop: [`agent-loop.md`](./agent-loop.md)
