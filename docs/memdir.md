---
title: Memdir (Auto-Memory Directory)
slug: memdir
priority: P3
status: not_started
spec: complete
code: not_started
package: internal/memdir
gaps:
  []
depends_on:
  - system-prompt
---
# Memdir (Auto-Memory Directory)

## Overview

Project-scoped memory directory with MEMORY.md index and topic files. Auto-created at prompt build.

## Location

`<config-home>/projects/<sanitized-git-root>/memory/`

Worktrees share canonical git root for path.

## MEMORY.md Caps

| Cap | Limit |
|-----|-------|
| Lines | 200 |
| Bytes | 25 KB |

Truncate line-first, then byte-at-last-newline. Warning names which cap fired.

## Memory Types

user, feedback, project, reference — index vs topic files; no duplicates.

## Freshness

Read of auto-mem files: prefix `<system-reminder>` when mtime >1 day.

## Disable Chain

- Env `DISABLE_AUTO_MEMORY`
- `--bare` mode
- Remote without memory dir
- Settings `autoMemoryEnabled: false`

## Security

Path validation for overrides. Project settings cannot set `autoMemoryDirectory`.

## Acceptance Criteria

- **AC1:** Dir created at prompt build if enabled.
- **AC2:** MEMORY.md enforces 200 lines and 25KB.
- **AC3:** Freshness note when mtime >1 day.
- **AC4:** Disabled in bare mode.
- **AC5:** Truncation warning identifies cap.
