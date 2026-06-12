---
title: File I/O Robustness
slug: file-io-robustness
status: done
priority: P0
spec: complete
code: done
package: internal/tool, internal/session, internal/agent
gaps: []
---

# File I/O Robustness

## Overview

This document outlines the architecture and implementation standards for robust file I/O, addressing concurrency, TOCTOU (Time-of-Check to Time-of-Use), and resource management issues.

## Canonical Acceptance Criteria

### AC1: TOCTOU & Cache Consistency (P0)

Given a file under concurrent modification: when an agent reads the file for the full read dedup path, the stat-and-cache-compare happens inside a single cache mutex operation (via `CheckAndRecord`). A concurrent writer cannot interleave a mtime update between the stat and the cache decision.

*Verification:* `TestReadTool_TOCTOU` injects a peer goroutine that modifies a file while `ReadTool.Execute` is invoked; the cache hit/miss decision is deterministic and never returns a stale "cached content is current" after a modification.

### AC2: Session Transcript Concurrency (P0)

Access to session transcript files is protected by a per-session `sync.RWMutex`. Concurrent `AppendEntry` (write-lock) and `LoadTranscript` (read-lock) do not collide and never produce a corrupted JSONL file.

*Verification:* `TestConcurrency` starts two goroutines: one appending 100 entries while the other concurrently loads the transcript. After both finish, the loaded transcript contains exactly the appended entries. No data race reported under `-race`.

### AC3: Resource-Aware Reads — OOM Prevention (P0)

The Read tool rejects files larger than 1 GiB (1,073,741,824 bytes) with a clear error message *before* reading any content. The check uses `os.Stat` and inspects `Size()` before opening the file for reading.

*Verification:* `TestReadTool_1GiBLimit` attempts to read a file ≥1 GiB and verifies `isError: true` with a message containing "too large". A 500 MiB file is read successfully.

### AC4: Atomic File Operations (P1)

The Write (and Edit) tool writes file content to a temporary file in the same directory, calls `Sync()` on it, then renames the temp file over the target path atomically. On cross-device rename, it falls back to copy-then-delete. A crash during the write never leaves a partially-written target file.

*Verification:* `TestAtomicWrite` and `TestAtomicEdit` verify that write operations use temp-file-then-rename. The temp file is cleaned up on success.

### AC5: Task Output Integrity (P1)

Task output files are opened with `os.O_APPEND` so multiple concurrent task writers never corrupt each other's output via interleaved `write` syscalls.

*Verification:* `TestTaskOutputAppendMode` verifies that `WriteTaskResult` uses `os.O_APPEND`. `TestConcurrentTaskWrites` writes from multiple goroutines and verifies all lines are intact.

### AC6: JSONL Integrity (P1)

When reading a JSONL transcript, malformed lines (invalid JSON) generate a logged warning (`slog.Warn`) instead of being silently skipped or crashing. The agent continues past the bad line.

*Verification:* `TestMalformedJSONLogging` inserts a non-JSON line into a transcript; `LoadTranscript` returns successfully with all valid entries; log output contains a warning message referencing the malformed line.

### AC7: UTF-8 Safe Truncation (P2)

Any code path that truncates strings for display or memory-directory summaries uses rune-aware slicing (`utf8.ValidString` or `[]rune`) so multi-byte code points are never split.

*Verification:* `TestUTF8SafeTruncate` verifies that a string containing a 4-byte emoji at a truncation boundary is never split mid-code-point.

### AC8: Recursive Traversal Limits (P2)

The Glob tool enforces both a maximum recursion depth (configurable, default 64) and a maximum result count (100, already implemented). Directory walks deeper than the limit are pruned without error.

*Verification:* `TestGlobTool_MaxDepthLimit` verifies that a directory tree 100 levels deep returns results only from the configured depth limit. `TestGlobTool_MaxResults` verifies the 100 result cap with `Truncated: true`.

## 🔴 Critical Standards (P0)

### 1. TOCTOU & Cache Consistency
- **Rule:** `os.Stat` and mtime comparisons MUST be performed inside the same lock that protects the file cache.
- **Reasoning:** Prevents race conditions where a stale mtime is cached between the check and the cache update.
- **Implementation:** See `internal/tool/read.go` and `internal/tool/readfile.go`.

### 2. Session Transcript Concurrency
- **Rule:** Access to session transcripts MUST be protected by a per-session `sync.RWMutex`.
- **Reasoning:** Ensures that JSONL line-appending (`AppendEntry`) and full-file reading (`LoadTranscript`) do not collide.
- **Implementation:** `internal/session/manager.go`.

### 3. Resource-Aware Reads (OOM Prevention)
- **Rule:** Check file size via `os.Stat` BEFORE reading the entire file into memory.
- **Threshold:** 1 GiB (hard limit).
- **Implementation:** `internal/tool/read.go`.

## 🟠 High Priority Standards (P1)

### 4. Atomic File Operations
- **Rule:** When modifying files, use a temporary file, `Sync()` it, and then rename it over the original.
- **Fallback:** Handle cross-device rename failures by falling back to copy+delete.
- **Implementation:** `internal/tool/edit.go`.

### 5. Task Output Integrity
- **Rule:** Use `os.O_APPEND` for task output writes to prevent corruption from multiple writers or partial flushes.
- **Implementation:** `internal/tool/task_manager.go`.

### 6. JSONL Integrity
- **Rule:** Log warnings for malformed JSON lines instead of silently skipping them to aid in diagnosing corruption.

## 🟡 Medium Priority Standards (P2)

### 7. UTF-8 Safe Truncation
- **Rule:** Use `utf8.ValidString` or rune-aware slicing when truncating strings for display or memory-directory summaries.

### 8. Recursive Traversal Limits
- **Rule:** Implement maximum depth and result count limits for recursive directory traversal (e.g., `GlobTool`).
- **Implementation:** `internal/tool/glob.go`.
