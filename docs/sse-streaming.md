---
title: SSE Streaming from API
slug: sse-streaming
priority: P0
status: not_started
spec: complete
code: not_started
package: internal/api
gaps:
  - Entire feature unimplemented; blocks include_partial_messages and live stream_event
depends_on:
  - anthropic-api-client
---
# SSE Streaming from API

## Overview

Primary API path uses server-sent events. Partial text accumulates per content block; on failure, fall back to non-streaming with bounded timeout.

## Stream Loop

- Request with `stream: true`.
- Use raw message stream events (avoid O(n²) partial JSON parser).
- Yield `{ type: stream_event, event: part }` for each SSE event.
- On `content_block_stop`: yield completed assistant block.
- On `message_delta`: update last assistant usage and stop_reason in place.

Idle watchdog: abort if no chunks within configured timeout.

## stream_request_start

Query loop yields `{ type: stream_request_start }` at **start of each API iteration** (before compact setup in that turn).

Headless stream-json may forward for request boundary markers.

## include_partial_messages

When enabled: forward raw `stream_event` to consumer.

When disabled: only completed assistant/user messages.

Partial assistant text for stream-json depends on this flag **and** live SSE.

## Non-Streaming Fallback

Trigger on: stream exception, idle timeout, incomplete stream (no message_start or no stop_reason), 404 at stream creation.

Fallback:

- Non-streaming API call; timeout ~5 min max.
- `onStreamingFallback`: tombstone partial assistant messages, discard streaming tool executor, clear pending tool_use IDs.
- Count streaming 529 toward 529 budget.
- Optional env to disable fallback (avoid double tool execution).

## Resource Cleanup

Always cancel response body on exit.

## Edge Cases

| Case | Expected behavior |
|------|-------------------|
| APIUserAbortError | Rethrow unless tool interrupt variant |
| Structured output turn 2 empty | Valid with stop_reason; no false incomplete |
| Fallback | Do not reuse partial tool_use IDs |

## Acceptance Criteria

- **AC1:** SSE default; text arrives via deltas.
- **AC2:** stream_request_start each iteration.
- **AC3:** Fallback completes via non-streaming on failure.
- **AC4:** Partial events only when flag enabled.
- **AC5:** Partial stream tombstoned before fallback retry.
