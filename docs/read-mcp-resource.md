---
title: ReadMcpResource Tool
slug: read-mcp-resource
priority: P2
status: partial
spec: complete
code: partial
package: internal/tool
gaps:
  - "Resource subscriptions (subscribe/unsubscribe) not implemented"
  - "Handling of notifications/resources/updated not implemented"
  - "Support for reading resource templates (URI templates) not implemented"
depends_on:
  - mcp-client
---
# ReadMcpResource Tool

## Overview

Fetches single MCP resource by server and URI. Binary content persisted to disk — never inline base64.

## Parameters

| Param | Description |
|-------|-------------|
| `server` | MCP server name (required) |
| `uri` | Resource URI (required) |

## Validation

- Unknown server → error with available servers.
- Not connected → error.
- Missing resources capability → error.

## Execution

MCP `resources/read` with URI.

Per content item:

| Type | Handling |
|------|----------|
| text | Pass through in result |
| blob (base64) | Decode, persist to disk, return path in text |

Persist failure → error string, no inline blob.

## Output

JSON: `{ contents: [{ uri, mimeType?, text?, blobSavedTo? }] }`

## Edge Cases

| Case | Expected behavior |
|------|-------------------|
| Multiple content parts | Process each independently |
| Oversized text | Subject to global tool result truncation |
| Unique persist IDs | timestamp + random suffix |

## Acceptance Criteria

- **AC1:** Unknown server errors clearly.
- **AC2:** Text inline in text field.
- **AC3:** Binary on disk; path in result only.
- **AC4:** Persist failure not base64 inline.
- **AC5:** Concurrency-safe read-only.
