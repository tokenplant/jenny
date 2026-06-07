---
title: MCP Client
slug: mcp-client
priority: P0
status: partial
spec: complete
code: partial
package: internal/mcp
gaps:
  - "SSE, HTTP, WebSocket transports not implemented (stdio only this iteration)"
  - "OAuth token refresh not implemented (no HTTP transport)"
  - "Binary MCP results not persisted to disk (text only)"
  - "Content truncation at MAX_MCP_OUTPUT_TOKENS not implemented"
  - "Resource cache not implemented"
  - "Progress events not implemented"
depends_on:
  - mcp-config
---
# MCP Client

## Overview

Jenny implements an MCP client that connects to configured servers, exposes their tools to the model, and handles auth, transport, and result size limits in headless mode.

## Transports

| Transport | Use case |
|-----------|----------|
| `stdio` | Default; spawn subprocess with stdin/stdout JSON-RPC |
| `sse` | Server-sent events endpoint |
| `http` | Streamable HTTP (modern MCP transport) |
| `ws` | WebSocket endpoint |

Connection lifecycle: connect → initialize → list tools/resources → ready.

## OAuth and 401 Handling

On HTTP 401 from MCP server:

1. Attempt token refresh via stored OAuth credentials.
2. Retry request once with refreshed token.
3. If refresh fails, mark server status `needs-auth` and surface error to operator (no interactive prompt in headless mode).

## Tool Naming

MCP tools are exposed to the model with normalized names:

```
mcp__<normalized_server>__<normalized_tool>
```

Normalization: lowercase, non-alphanumeric → underscore, collapse repeats.

Example: server `My Server`, tool `List Files` → `mcp__my_server__list_files`

## Binary Results

Binary MCP content must **not** be inlined as base64 in tool_result text.

Flow:

1. Decode blob from MCP response.
2. Persist to disk under session-scoped tool-results directory.
3. Return human-readable path reference in tool_result.

Applies to `ReadMcpResource` and MCP tool calls returning binary.

## Content Truncation

Oversized MCP text responses truncate before model context:

- Default cap: **25,000 output tokens** (`MAX_MCP_OUTPUT_TOKENS`).
- Truncation appends notice with original size.
- Truncated content still valid JSON/text where applicable.

## Resource Cache

Per-server LRU cache for `resources/list` and `resources/read`.

Invalidate cache on:

- Server disconnect
- Session expired
- `notifications/resources/list_changed`

## Progress Events

During long MCP tool calls, emit progress entries (not chain nodes):

- `mcp_progress` with `status: started | completed`
- Yield separately from final tool_result in stream-json

## Edge Cases

| Case | Expected behavior |
|------|-------------------|
| Server disconnect mid-call | Error tool_result; invalidate cache |
| Tool renamed on server | Refresh tool list on reconnect |
| Multiple content parts (text + blob) | Process each; blob to disk |
| Persist failure for binary | Error text; never inline base64 |
| Concurrent MCP calls same server | Serialize or use connection pool per server policy |

## Headless Protocol Compatibility

- `system`/`init` includes `mcp_servers: [{ name, status }]`.
- Tool progress may appear as `tool_progress` lines between assistant and user messages.
- Final tool_result uses text/path only, compatible with stream-json `user` message shape.

## Acceptance Criteria

- **AC1:** All four transports connect with valid config.
- **AC2:** 401 triggers refresh flow once before marking needs-auth.
- **AC3:** Binary MCP output persisted to disk; tool_result references path.
- **AC4:** Text exceeding token cap truncated with notice.
- **AC5:** Resource cache cleared on disconnect.
