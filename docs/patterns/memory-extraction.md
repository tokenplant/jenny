---
title: Memory Extraction
slug: memory-extraction
priority: P3
status: done
spec: complete
code: done
package: internal/agent
gaps:
  []
depends_on:
  - memdir
---
# Memory Extraction

## Overview

End-of-turn forked agent extracts durable memories to auto-memory directory. Runs after turn completes, not mid-loop.

## Timing

- Run at stop hooks when no pending tool calls.
- Main agent only (not subagents).
- Throttle: every N eligible turns (default 1).

## Fork Configuration

- `skipTranscript: true`
- `maxTurns: 5`
- `querySource: extract_memories`

## Mutual Exclusion

If main agent wrote to auto-mem paths since cursor → skip fork, advance cursor only.

## Cursor

- `lastMemoryMessageUuid`
- UUID missing after compaction → count all model-visible messages (do not permanently disable)

## Coalescing

In-progress runs stash latest context for one trailing run.

## Permissions (forked agent)

- Read/Grep/Glob unrestricted
- Read-only Bash
- Edit/Write only under auto-mem dir

## Shutdown

Drain with 60s soft timeout before exit.

Pre-inject memory manifest to avoid extra ls turn.

## Acceptance Criteria

- **AC1:** Runs end-of-turn only.
- **AC2:** Skips when main agent wrote memory in range. The test blocks on the channel with a 3-second timeout, then checks coalescing.
- **AC3:** Compaction cursor fallback by message count.
- **AC4:** Edit scoped to auto-mem dir. The second select uses 50ms timeout to confirm coalescing (not 500ms). Production code runs a trailing extraction ~100-200ms after the first, so 50ms avoids false positives.
- **AC5:** Coalesces concurrent extraction requests.
