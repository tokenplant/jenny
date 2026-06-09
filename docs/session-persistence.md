---
title: Session Persistence
slug: session-persistence
priority: P0
status: done
spec: complete
code: done
package: internal/session
gaps: []
depends_on:
  - session-id-stability
---
# Session Persistence

## Overview

Jenny persists conversation state as append-only JSONL transcripts under the project directory. Transcripts are the source of truth for resume, cost restoration, and headless consumers that round-trip `session_id`.

## Envelope Fields

Every line in a transcript JSONL file carries two mandatory envelope fields:

| Field | Type | Description |
|-------|------|-------------|
| `session_id` | string | Session ID; equal to the JSONL filename stem (filename without `.jsonl`) |
| `uuid` | string | Lowercase UUID v4 matching `^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$` |
| `cwd` | string | Absolute path of the working directory at session start |

All three fields must be non-empty on every line. The `session_id` and `cwd` values are consistent across all lines within one session run.

## Transcript Location

```
.jenny/transcripts/<session_id>.jsonl
```

Each line is one JSON object. The directory is created on first write.

## Chain Participants vs Non-Chain Entries

Not every persisted line becomes an API message on reload.

### Chain participants (rebuild conversation)

| `type` | Role in chain |
|--------|---------------|
| `user` | User turn |
| `assistant` | Model turn (may include `tool_use`) |
| `attachment` | Attachment context |
| `system` | Selected system subtypes (e.g. compact boundary) |

### Non-chain entries (persist but do not fork conversation)

| Category | Examples | Behavior on reload |
|----------|----------|-------------------|
| Progress / ephemeral | `progress`, `bash_progress`, `powershell_progress`, `mcp_progress` | UI/telemetry only; skipped for API chain |
| Metadata | `queue-operation`, `custom-title`, `tag`, `file-history-snapshot`, `content-replacement` | Stored; not sent to model |
| Tombstones | Deleted message markers | Remove referenced UUID from chain |

**Critical:** Progress messages must never become chain nodes. Reloading a transcript with progress entries must not fork or duplicate the conversation.

Legacy transcripts may contain `type: "progress"` entries; on load, rewire `parentUuid` to the nearest chain participant.

## Write Path

1. Append each turn after it completes (buffered write queue per project).
2. Use append mode for crash recovery (partial last line may be discarded on parse).
3. Register shutdown hook to **flush** pending writes before exit.

## Tombstone Deletion

When a message is deleted mid-session, the storage layer may rewrite the transcript to remove it.

- **Fast path:** Tail splice when deletion is near end of file.
- **Slow path:** Full rewrite when deletion is earlier in file.
- **OOM guard:** Full rewrite capped at **50 MiB** (`MAX_TOMBSTONE_REWRITE_BYTES`). Beyond this cap, refuse rewrite or use alternative strategy that does not load entire file into memory.

## Persistence Disable

When persistence is disabled (e.g. `--no-session-persistence`):

- Skip all transcript writes.
- Resume (`-r`) must fail or start fresh (no read from disk).
- Headless print mode only in reference behavior.

## Edge Cases

| Case | Expected behavior |
|------|-------------------|
| Malformed JSONL line | Skip line; log warning; continue parsing |
| Multi-GB transcript | Tombstone rewrite must respect 50 MiB cap |
| Process killed mid-write | Resume from last complete lines; skip partial tail |
| `sessionProjectDir` vs cwd drift | Resolve project dir consistently at save and load |
| Concurrent sessions same ID | Last writer wins; document as undefined (single process assumed) |

## Headless Protocol Compatibility

- `session_id` in stream-json output must match the transcript filename stem exactly.
- Terminal `result` line must include the same `session_id` as `system`/`init`.

## Acceptance Criteria

- **AC1:** Chain rebuild includes only user/assistant/attachment/system participants; progress types excluded.
- **AC2:** Tombstone full rewrite refuses or streams when file exceeds 50 MiB.
- **AC3:** Shutdown flush completes before process exit when persistence enabled.
- **AC4:** With persistence disabled, no files written under `.jenny/transcripts/`.
- **AC5:** Append-only writes survive normal crash (at most one partial line lost).
- **AC6:** The `session_id` emitted in the stream-json `system` event and `result` event equals the stem of the `.jsonl` transcript file created in the same run.
- **AC7:** Every transcript line has a non-empty `cwd` field equal to the absolute path of the directory from which jenny was invoked.

## Related

- Resume behavior: [`session-resume.md`](./session-resume.md)
- Cost restore: [`cost-tracking.md`](./cost-tracking.md)
