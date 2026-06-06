---
title: WebFetch Tool
slug: web-fetch
priority: P3
status: not_started
spec: complete
code: not_started
package: internal/tool
gaps:
  []
depends_on:
  - tool-registry
---
# WebFetch Tool

## Overview

Fetches URL content, converts HTML to markdown, applies optional prompt to extracted text.

## Parameters

| Param | Description |
|-------|-------------|
| `url` | HTTP(S) URL (max 2000 chars) |
| `prompt` | Applied to fetched content via secondary model (unless preapproved) |

## Limits

| Limit | Value |
|-------|-------|
| Response body | 10 MB |
| Timeout | 60s |
| Redirect hops | 10 |
| Result markdown | 100K chars |
| URL length | 2000 chars |

Reject credentials in URL.

## Behavior

- HTML → markdown (turndown).
- Domain blocklist preflight (10s timeout).
- Cache: 15 min / 50 MB LRU; hostname cache 5 min.
- Cross-host redirect → instruct model to re-fetch redirect URL (no auto cross-host follow).
- Binary saved to disk with path note.

## Permissions

Per-hostname `domain:<host>`. Preapproved hosts bypass gate.

Auth warning in tool description.

## Acceptance Criteria

- **AC1:** 10MB body limit enforced.
- **AC2:** HTML capped at 100K chars markdown.
- **AC3:** Blocklist preflight before fetch.
- **AC4:** Cross-host redirect returns re-fetch instruction.
- **AC5:** Credentials in URL rejected.
