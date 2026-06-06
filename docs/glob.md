---
title: Glob Tool
slug: glob
priority: P1
status: not_started
spec: complete
code: not_started
package: internal/tool
gaps:
  []
depends_on:
  - tool-registry
---
# Glob Tool

## Overview

Glob finds files matching a pattern. Read-only, concurrency-safe.

## Parameters

| Param | Description |
|-------|-------------|
| `pattern` | Glob pattern (e.g. `**/*.go`) |
| `path` | Directory to search (optional, default cwd) |

## Behavior

- Default max **100** results.
- Set `truncated: true` in result when capped.
- Return paths **relative to cwd** for token savings.
- Sort by modification time (newest first) unless specified otherwise.

## Validation

- `path` if provided: must exist and be directory.
- ENOENT: error with cwd suggestion.
- Empty result: tool result text `"No files found"` (not empty string).

## Properties

- Concurrency-safe: `isConcurrencySafe() === true`
- Read-only: no filesystem mutations

## Edge Cases

| Case | Expected behavior |
|------|-------------------|
| Pattern matches >100 files | Return 100 + truncated flag |
| Invalid pattern syntax | Clear error |
| Symlink directories | Follow policy consistent with search tools |

## Acceptance Criteria

- **AC1:** Max 100 results with truncated flag when exceeded.
- **AC2:** Paths relative to cwd.
- **AC3:** Empty → "No files found".
- **AC4:** Non-directory path errors.
- **AC5:** Safe for parallel execution.
