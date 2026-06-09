---
title: Message Normalization
slug: message-normalization
priority: P2
status: done
spec: complete
code: done
package: internal/agent
gaps: []
depends_on:
  - anthropic-api-client
---
# Message Normalization

## Overview

Before each API request, convert internal transcript messages to API-safe payloads: strip internal fields, merge roles, enforce tool pairing, format Read output.

## Strip Internal Content

Drop from API send:

- `progress`, most system subtypes (except allowed).
- Synthetic API error messages.
- Virtual (`isVirtual`) user/assistant messages.
- Non-API fields on tool_use (e.g. `caller` when tool search off).
- `tool_reference` blocks when tool search off or tool disconnected.

## Tool Result Pairing

`ensureToolResultPairing()`:

| Direction | Action |
|-----------|--------|
| Forward | Synthetic error result for missing tool_use_id |
| Reverse | Strip orphaned tool_results |
| Duplicate IDs | Dedupe across messages |
| Leading orphaned user tool_result | Strip or placeholder text |
| Empty assistant after strip | Insert `[Tool use interrupted]` text |

**Strict mode:** throw on mismatch instead of repair.

## Role Merging

- Consecutive user messages → merge (Bedrock compatibility).
- Consecutive assistant with same `message.id` → merge streaming chunks.

## Read Output Format

- `offset=1` default (1-based); `offset=0` → line 1.
- Line numbers: compact `{n}\t{text}` or legacy padded format.
- Empty content: warning, not error.
- Past EOF: warning with actual line count.

## Media Error Mapping

| API pattern | User-facing string |
|-------------|-------------------|
| Image size / resize | getImageTooLargeErrorMessage |
| PDF page limit | getPdfTooLargeErrorMessage |
| Password PDF | getPdfPasswordProtectedErrorMessage |
| Invalid PDF | getPdfInvalidErrorMessage |
| 413 request too large | getRequestTooLargeErrorMessage |

Strip offending media from meta user message on retry.

## Thinking Normalization Order

1. Orphaned thinking filter
2. Trailing thinking strip
3. Whitespace-only filter
4. Non-empty assistant guard
5. Tool pairing

## Edge Cases

| Case | Expected behavior |
|------|-------------------|
| is_error tool_result | Inner content text-only |
| Resume mid-turn tool_result only | Repair without assistant-first payload |
| Snip runtime tags | Append [id:xxx] to user messages (non-test) |

## Acceptance Criteria

- **AC1:** No internal UUID/timestamp in API JSON.
- **AC2:** Every tool_use has matching tool_result.
- **AC3:** Read uses fixed-width line numbers.
- **AC4:** Media errors map to specific strings.
- **AC5:** Last assistant never ends with thinking block.
