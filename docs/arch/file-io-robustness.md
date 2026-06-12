---
title: File I/O Robustness
slug: file-io-robustness
status: partial
priority: P0
spec: complete
code: partial
package: internal/tool, internal/session, internal/agent
---

# File I/O Robustness

## Overview

This document outlines the architecture and implementation standards for robust file I/O, addressing concurrency, TOCTOU (Time-of-Check to Time-of-Use), and resource management issues.

## 🔴 Critical Standards (P0)

### 1. TOCTOU & Cache Consistency
- **Rule:** `os.Stat` and mtime comparisons MUST be performed inside the same lock that protects the file cache.
- **Reasoning:** Prevents race conditions where a stale mtime is cached between the check and the cache update.
- **Implementation:** See `internal/tool/read.go`.

### 2. Session Transcript Concurrency
- **Rule:** Access to session transcripts MUST be protected by a per-session `sync.RWMutex`.
- **Reasoning:** Ensures that JSONL line-appending (`AppendEntry`) and full-file reading (`LoadTranscript`) do not collide.
- **Implementation:** `internal/session/manager.go`.

### 3. Resource-Aware Reads (OOM Prevention)
- **Rule:** Check file size via `os.Stat` BEFORE reading the entire file into memory.
- **Threshold:** 1 GiB (hard limit).
- **Implementation:** `internal/tool/edit.go`, `internal/tool/read.go`.

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
