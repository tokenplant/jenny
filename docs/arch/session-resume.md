---
title: Session Resume
slug: session-resume
priority: P0
status: done
spec: complete
code: done
package: internal/agent, internal/session
gaps: []
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
Restore system prompt (CachedSystemPrompt) from transcript state entry
    │
    ▼
Restore caches (readFileState, cost, compaction boundaries)
    │
    ▼
Continue with user prompt
```

## System Prompt Restoration

The frozen system prompt is persisted as a transcript `state` entry with field `SystemPrompt`:

```
{"type":"state","system_prompt":"You are an expert software engineering assistant with tools..."}
```

On resume loaded at `engine.go:131-149`:
1. Scan transcript for latest `state` entry with non-empty `SystemPrompt`
2. Set `cfg.CachedSystemPrompt` to the loaded value
3. Subsequent `AssembleSystemPrompt` calls return the frozen string

This ensures the system prompt prefix is byte-for-byte identical across process boundaries, enabling prompt caching hits even on resumed sessions.

## Queue-Only Filtering

If a transcript contains only `queue-operation` entries and zero chain messages, treat as **no conversation found** — return error, do not start empty chain silently.

> **Note:** The check is implemented at the CLI resume entrypoint (`cmd/jenny/main.go`) after `LoadTranscript` returns. It validates that at least one chain-participant entry (`user`, `assistant`, `tool_result`) exists before proceeding. Progress/ephemeral types (`progress`, `bash_progress`, `mcp_progress`, `powershell_progress`) and state types (`worktree_state`, `session_state`) do not count as chain participants. This rule supersedes the literal "queue-operation" wording in the spec — any session whose rebuilt message history would be empty is rejected.
>
> Helper-level (`TestHasChainMessages_TableDriven` in `internal/agent/loop_test.go`) and CLI-resume tests (`TestResume_QueueOnlyTranscript_Error`, `TestResume_EmptyTranscript_Error`, `TestResume_NormalTranscript_NoError`, `TestResume_ForkSession_NoFileCreated`, and `TestResume_NormalTranscript_ForkSession_CreatesFile` in `cmd/jenny/main_test.go`) directly exercise `HasChainMessages` for comprehensive regression coverage.

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

## Prompt Cache Fidelity

`RebuildMessages` must reproduce byte-for-byte identical API messages to what was sent in the original session. Any divergence invalidates the prompt cache prefix, wasting tokens and money.

Preserved fields that affect caching:

| Field | Where persisted | How restored |
|-------|----------------|--------------|
| `tool_result.IsError` | `TranscriptEntry.IsError` | `ToolResultBlock.IsError` in rebuilt message |
| `assistant.Thinking` | `TranscriptEntry.Thinking` | `Message.Thinking` |
| `assistant.Signature` | `TranscriptEntry.Signature` | `Message.Signature` |
| System prompt | `state` entry with `SystemPrompt` | `cfg.CachedSystemPrompt` |

System prompt suffix (dynamic content like git status, cwd) is sent as a **separate** content block (Anthropic), separate system message (OpenAI), or separate part (GenAI) to avoid invalidating the frozen prefix cache.

Known limitation: when the assistant returns multiple thinking blocks with distinct per-block signatures, only the last signature is preserved. This can cause a cache miss on resume if Anthropic validates per-block signatures in the history.

## Acceptance Criteria

- **AC1:** `-r` round-trips message history including tool_use/tool_result pairing.
- **AC2:** `readFileState` warm after resume enables Write/Edit staleness checks without re-read.
- **AC3:** Cost counters restore only on matching session ID.
- **AC4:** Queue-only transcripts rejected.
- **AC5:** Compaction boundaries truncate pre-boundary messages from API payload.
- **AC6:** `tool_result.IsError` is preserved through transcript round-trip to avoid cache invalidation.
- **AC-e2e-1:** Running `jenny -r <session_id> -p "<new prompt>"` with a valid prior transcript causes the Anthropic API request to include the prior user and assistant turns before the new user message.
- **AC-e2e-2:** The resumed run exits 0 and emits an `assistant` event on stdout.
