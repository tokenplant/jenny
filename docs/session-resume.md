---
title: Session Resume
slug: session-resume
priority: P0
status: partial
spec: complete
code: partial
package: internal/agent, internal/session
gaps:
  - Queue-only filtering missing
  - Compaction boundaries missing
  - --continue not implemented
defer_to: P3
depends_on:
  - session-persistence
  - cli
---
# Session Resume

## Overview

Jenny resumes prior conversations via `-r <session_id>`. Resume rebuilds API message history from the JSONL transcript and restores session-scoped caches where the session ID matches.

## CLI Flags

| Flag | Behavior |
|------|----------|
| `-r`, `--resume <session_id>` | Load transcript for given ID; continue with same ID |
| `--continue` | Resume most recent session in project (no ID required) |
| `--fork-session` | Copy history into a new session ID |
| `--resume-session-at <message_id>` | Truncate chain at given message UUID (requires `--resume`) |
| `--no-session-persistence` | Disables load/write (incompatible with resume) |

## Load Flow

```
-r session_id
    │
    ▼
Read .jenny/transcripts/<session_id>.jsonl
    │
    ▼
Parse chain participants (see session-persistence.md)
    │
    ▼
Filter queue-only / empty sessions
    │
    ▼
Restore caches (readFileState, cost, compaction boundaries)
    │
    ▼
Continue with user prompt
```

## Queue-Only Filtering

If a transcript contains only `queue-operation` entries and zero chain messages, treat as **no conversation found** — return error, do not start empty chain silently.

## readFileState Restoration

Seed the read-before-write cache from prior Read/Write/Edit tool_use + tool_result pairs in the transcript:

- Extract path, offset, limit, mtime, and content snapshot from completed reads.
- Write/Edit entries update cache after successful tool_result.
- Partial reads (`offset`/`limit` set) mark entries as partial — Write/Edit must reject partial views.

On resume, `QueryEngine` clones this cache at start and writes back at end of turn.

## Cost State Restoration

Restore accumulated token/cost counters **only when** persisted `lastSessionId` equals the resumed session ID.

If IDs mismatch (user passed different `-r` than stored cost metadata), start cost counters at zero.

Fields restored per model: input/output tokens, cache read/creation tokens, USD cost, API duration.

## Compaction Boundaries

Transcripts may contain `system` entries with subtype `compact_boundary` and `compact_metadata` (`trigger`, `pre_tokens`, `preserved_segment`).

On load:

1. Emit `system`/`compact_boundary` to stream-json consumers when replaying.
2. Splice pre-boundary messages from in-memory chain (only post-boundary content goes to API).
3. Reset file-history commits before boundary marker.

## Deserialize Filters

Drop or repair on load:

| Condition | Action |
|-----------|--------|
| Unresolved `tool_use` (no matching result) | Drop or synthesize error result |
| Orphaned thinking-only assistant | Drop |
| Whitespace-only assistant | Drop |
| Trailing user with no assistant response | Detect interrupt; may synthesize assistant placeholder |
| Duplicate tool_use IDs | Dedupe |

## Fork Session

`--fork-session` with `-r`:

- Read source transcript.
- Assign new session ID.
- Write new transcript file; do not mutate source.

## Edge Cases

| Case | Expected behavior |
|------|-------------------|
| Transcript file missing | Error: `session not found: <id>` |
| `--resume-session-at` invalid UUID | Error with available message IDs |
| Resume mid-tool-turn (pending tool_use) | Repair pairing or reject with clear error |
| Compaction + resume | Post-boundary only in API payload |
| Session ID path traversal | Reject malicious IDs before filesystem access |

## Headless Protocol Compatibility

- Resumed runs emit same stream-json sequence: `system`/`init` with original or forked `session_id`.
- `result` line usage includes tokens accumulated across prior + current invocation when cost restored.

## Acceptance Criteria

- **AC1:** `-r` round-trips message history including tool_use/tool_result pairing.
- **AC2:** `readFileState` warm after resume enables Write/Edit staleness checks without re-read.
- **AC3:** Cost counters restore only on matching session ID.
- **AC4:** Queue-only transcripts rejected.
- **AC5:** Compaction boundaries truncate pre-boundary messages from API payload.
