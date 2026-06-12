---
title: Session Memory
slug: session-memory
priority: P3
status: done
spec: complete
code: done
package: internal/agent
gaps:
  - "Natural break detection: not yet implemented"
depends_on:
  - context-compaction
---
# Session Memory

## Overview

Background markdown notes file maintained by a forked sub-agent on the main thread. Updates incrementally as session grows.

## Thresholds (defaults)

| Event | Threshold |
|-------|-----------|
| Init | ~10K context tokens |
| Update | Every ~5K token growth **and** 3 tool calls |
| Natural break | ~5K tokens when last assistant has no pending tool calls |

Token counting matches autocompact: input + output + cache tokens.

## Extraction

- Wait timeout: **15s**
- Stale in-flight (>60s): do not wait
- Update `lastSummarizedMessageId` only when last turn has no tool calls (avoid orphaned tool_result)

## Forked Agent Constraints

- May **Edit only** the session memory file.
- Uses forked agent path for prompt-cache sharing.
- Gated on auto-compact enabled; skip in remote mode.

## Edge Cases

| Case | Expected behavior |
|------|-------------------|
| Remote config zero values | Do not override defaults |
| Manual extraction | Bypass thresholds |
| First run | Create file with template (mode 0600) |
| Read dedup | Invalidate readFileState before read |

## Acceptance Criteria

- **AC1:** Init at ~10K tokens.
- **AC2:** Update respects token + tool call thresholds.
- **AC3:** 15s extraction timeout.
- **AC4:** Forked agent Edit-only on memory file.
- **AC5:** Disabled when auto-compact off.
- **AC6:** Retain at most 200 most-recent memory files (configurable via `JENNY_SESSION_MEMORY_RETENTION`).
