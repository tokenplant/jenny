---
title: OpenAI API Client
slug: openai-api-client
priority: P0
status: done
spec: complete
code: done
package: internal/api
gaps: []
depends_on:
  - anthropic-api-client
---

# OpenAI API Client

## Overview

The OpenAI provider implements the `Provider` interface using the OpenAI Chat Completions API. It is designed to be wire-compatible with OpenAI-compatible proxies and backends.

## Selection Logic

The OpenAI provider is selected if the `OPENAI_BASE_URL` environment variable is set. It takes precedence over the default Anthropic provider.

## Configuration

| Environment Variable | Description | Default |
|----------------------|-------------|---------|
| `OPENAI_BASE_URL` | Base URL for the API (e.g., `https://api.openai.com/v1`) | (Required for selection) |
| `OPENAI_API_KEY` | API key for authentication | (Required) |
| `OPENAI_DEFAULT_MODEL`| Model name to use if not specified | (Required) |
| `OPENAI_WIRE_API` | Wire protocol version (`chat` supported) | `chat` |

## Mapping

### Roles
- `user` -> `user`
- `assistant` -> `assistant`
- `system` -> `system` (passed as the first message in the array)
- `tool` -> `tool` (OpenAI uses `role: tool` with `tool_call_id`)

### Stop Reasons
| OpenAI `finish_reason` | Jenny `StopReason` |
|------------------------|-------------------|
| `stop` | `end_turn` |
| `tool_calls` | `tool_use` |
| `length` | `max_tokens` |
| (other) | (passthrough) |

## Normalization

The OpenAI provider utilizes the `NormalizeMessages` pipeline to ensure payload compatibility, specifically:
- **Tool Result Dedup:** Ensures `tool_call_id` matches.
- **Role Alternation:** Merges consecutive messages of the same role.

## Streaming

Streaming uses Server-Sent Events (SSE). The implementation must yield partial content blocks as they arrive from the network without buffering the full response.

### Known Deficiencies (Fixing)
- Current `SSEReader` implementation uses `io.ReadAll`, which breaks streaming semantics by waiting for the entire body.

## Acceptance Criteria

- **AC1:** Selected automatically when `OPENAI_BASE_URL` is present.
- **AC2:** Correctly maps Anthropic-style tool results to OpenAI `role: tool` messages.
- **AC3:** Supports real SSE streaming without full-body buffering.
- **AC4:** Handles `OPENAI_WIRE_API=chat` (Responses API explicitly unsupported).
