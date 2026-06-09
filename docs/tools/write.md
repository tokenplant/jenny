---
title: Write Tool
slug: write
priority: P1
status: done
spec: complete
code: done
package: internal/tool
gaps:
  []
depends_on:
  - read
  - query-engine
---
# Write Tool

## Overview

Write creates or overwrites files. Requires prior Read of same path with matching mtime (read-before-write contract).

## Parameters

| Param | Description |
|-------|-------------|
| `file_path` | Target path |
| `content` | Full file content |

## Read-Before-Write

1. `readFileState` must contain path from prior Read in this session.
2. Reject if entry is partial view (`offset`/`limit` set on read).
3. Staleness: if `mtime > readTimestamp` → error (file changed since read).
4. Windows: content-compare fallback for full reads when mtime unreliable.

## Write Behavior

- Create parent directories (`mkdir -p`).
- Always write **LF** line endings.
- Return structured patch diff in tool result.

## Post-Write Updates

- Update `readFileState` with new content and mtime.
- LSP `didChange` notification if LSP connected.
- File history tracking.
- Skill directory discovery.

## UNC Paths (Windows)

Skip pre-validation I/O on UNC paths (NTLM leak prevention).

## Edge Cases

| Case | Expected behavior |
|------|-------------------|
| Write without Read | Error code indicating read first |
| Partial read then Write | Reject partial view |
| Concurrent external edit | mtime staleness error |
| New file | Read may use empty content snapshot |

## Acceptance Criteria

- **AC1:** Write without readFileState entry fails.
- **AC2:** Stale mtime fails before write.
- **AC3:** Parent dirs created automatically.
- **AC4:** Result includes patch diff.
- **AC5:** readFileState updated after success.
