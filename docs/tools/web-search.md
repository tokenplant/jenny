---
title: WebSearch Tool
slug: web-search
priority: P3
status: done
spec: complete
code: done
package: internal/tool
gaps: []
depends_on:
  - tool-registry
  - anthropic-api-client
---
# WebSearch Tool

## Overview

Server-side web search via provider tool schema. Gated by provider/model support.

## Parameters

| Param | Description |
|-------|-------------|
| `query` | Search query (min length 2) |
| `allowed_domains` | Restrict results (mutually exclusive with blocked) |
| `blocked_domains` | Exclude domains |

## Limits

- Max **8** server searches per invocation.
- Query min length **2**.

## Provider Gating

Enabled for supported first-party / vertex / foundry model combinations only.

Uses server-side `web_search_20250305` tool schema internally.

## Errors

Surface server `error_code` as string in result.

Progress events for query updates and result counts.

## Acceptance Criteria

- **AC1:** Query length ≥2.
- **AC2:** Max 8 searches per call.
- **AC3:** allowed_domains XOR blocked_domains.
- **AC4:** Unsupported model returns clear error.
- **AC5:** Server error codes in result text.
