---
title: ListMcpResources Tool
slug: list-mcp-resources
priority: P2
status: partial
spec: complete
code: partial
package: internal/mcp
gaps:
  - "Cursor-based pagination (pagination/list) not implemented"
  - "Handling of notifications/resources/list_changed to invalidate cache not implemented"
  - "Resource templates not included in listing"
depends_on:
  - mcp-client
---
# ListMcpResources Tool

## Overview

Read-only listing of MCP resources from connected servers. Optional server filter.

## Parameters

| Param | Description |
|-------|-------------|
| `server` | Optional filter to one MCP server name |

## Behavior

- `server` set but no match → **error** listing available server names.
- Per connected server: fetch resources (LRU cache).
- Per-server failure → `[]` for that server (not whole-call failure).
- Disconnected clients skipped.

## Output

Empty aggregate: note that resources may be empty while tools still exist.

Non-empty: JSON array with `uri`, `name`, optional `mimeType`/`description`, `server`.

## Properties

- Concurrency-safe, read-only.
- Deferrable in tool search.

## Edge Cases

| Case | Expected behavior |
|------|-------------------|
| Cache stale | Invalidate on disconnect, resources/list_changed |
| Unknown server | Error with server list |
| Known server zero resources | Empty array + note |

## Acceptance Criteria

- **AC1:** No filter returns all connected servers' resources.
- **AC2:** Invalid server errors with available names.
- **AC3:** Partial failure returns partial results.
- **AC4:** Empty result includes tools-may-exist note.
- **AC5:** Each entry includes server field.
