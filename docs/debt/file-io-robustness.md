---
title: File I/O Robustness Remediation
slug: file-io-robustness
status: complete
priority: P0
spec: complete
code: complete
package: internal/tool, internal/session, internal/agent
date_completed: 2026-06-12
---

# File I/O Robustness Remediation Plan

This document outlines the plan to address critical concurrency, TOCTOU, and resource management issues identified during the File Operation Analysis (June 2026).

## Overview

The analysis identified 20 issues across 24 files, ranging from high-risk race conditions to memory efficiency concerns. The primary focus is on ensuring system integrity, thread safety, and crash resilience.

## 🔴 Critical Fixes (P0)

### 1. TOCTOU & Cache Inconsistency in `read.go`
- **Issue:** `os.Stat` is called outside the cache mutex, leading to potential race conditions where a stale mtime is cached.
- **Strategy:** Move all `os.Stat` calls and mtime comparisons inside the `ReadFileCache` lock.
- **Target:** `internal/tool/read.go`

### 2. Session Transcript Concurrency in `session/manager.go`
- **Issue:** No synchronization between JSONL line-appending (`AppendEntry`) and full-file reading (`LoadTranscript`).
- **Strategy:** Implement a per-session `sync.RWMutex` to protect transcript file access.
- **Target:** `internal/session/manager.go`

### 3. Memory Extraction Goroutine Leak & Race
- **Issue:** Unmanaged goroutines and reference capture of `turnCtx`.
- **Strategy:** 
  - Use `sync.WaitGroup` to track extraction goroutines.
  - Implement proper `Drain` logic during shutdown.
  - Capture `turnCtx` by value or deep-copy pointers.
- **Target:** `internal/agent/memory_extraction.go`

### 4. `EditTool` OOM Prevention
- **Issue:** Files are read into memory before checking the 1 GiB size limit.
- **Strategy:** Check `info.Size()` from `os.Stat` *before* calling `os.ReadFile`.
- **Target:** `internal/tool/edit.go`

## 🟠 High Priority (P1)

### 5. Atomic File Operations in `EditTool`
- **Issue:** Potential data loss if `os.Rename` fails across file systems or if `Sync()` is skipped.
- **Strategy:** 
  - Call `tmpFile.Sync()` before closing.
  - Handle cross-device rename by falling back to copy+delete if necessary.
- **Target:** `internal/tool/edit.go`

### 6. Task Output Corruption
- **Issue:** `WriteTaskResult` and `FlushPartialOutput` use truncation instead of appending.
- **Strategy:** Use `os.O_APPEND` flag for all task output writes.
- **Target:** `internal/tool/task_manager.go`

### 7. JSONL Integrity
- **Issue:** Silent skipping of malformed JSON lines in session logs.
- **Strategy:** Add structured logging (Warn) for unmarshal failures to aid debugging.
- **Target:** `internal/session/manager.go`

## 🟡 Medium Priority (P2)

### 8. UTF-8 Safe Truncation
- **Issue:** Byte-level truncation in `memdir.go` can break multi-byte characters.
- **Strategy:** Use `utf8.ValidString` or rune-aware slicing for truncation.
- **Target:** `internal/memdir/memdir.go`

### 9. Resource Limits in `GlobTool`
- **Issue:** `filepath.Walk` lacks depth or count limits.
- **Strategy:** Implement a maximum depth/result limit for recursive directory traversal.
- **Target:** `internal/tool/glob.go`

## Implementation Workflow

For each P0/P1 issue:
1. **Reproduction:** Write a test case in the corresponding `*_test.go` file that reproduces the race or failure.
2. **Implementation:** Apply the surgical fix.
3. **Validation:** Run the reproduction test and the full package test suite.
4. **Verification:** Confirm no regressions in related tools.

## Tracking

| Task | Priority | Status | Owner |
|------|----------|--------|-------|
| read.go TOCTOU | P0 | ✅ Complete | |
| session.go Mutex | P0 | ✅ Complete | |
| extraction.go Leak | P0 | ✅ Complete | |
| edit.go OOM check | P0 | ✅ Complete | |
| task.go Append | P1 | Not Started | |
| atomic rename fix | P1 | Not Started | |
