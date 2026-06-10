---
title: Edit Tool
slug: edit
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
# Edit Tool

## Overview

Edit performs exact string replacement in files. Same read-before-write and staleness rules as Write.

Supports **scoped editing** via `start_line`/`end_line` to modify a specific line range after a partial read, and **`num_expected`** to guard against accidental multi-match corruption.

## Parameters

| Param | Description |
|-------|-------------|
| `file_path` | Target path |
| `old_string` | Text to find |
| `new_string` | Replacement text |
| `replace_all` | Replace all occurrences (required when multiple matches) |
| `start_line` | First line (1-indexed) of scoped replacement range. Required when editing after partial read. |
| `end_line` | Last line (1-indexed, inclusive) of scoped replacement range. Required when `start_line` is provided. |
| `num_expected` | Expected number of replacements. If actual count differs, the operation is aborted. |

## Limits

- Max file size: **1 GiB** (stat bytes) for global edit; scoped edit uses streaming I/O with no size limit.
- Partial read blocks edit unless `start_line`/`end_line` are provided and contained within the read range.

## Validation

| Condition | Result |
|-----------|--------|
| `old_string === new_string` | Rejected |
| `old_string === ''` on missing file | Create file |
| `old_string === ''` on non-empty file | Error |
| Multiple matches without `replace_all` | Error requiring replace_all |
| `.ipynb` path | Redirect to NotebookEdit tool |
| `end_line < start_line` | Error |
| `num_expected` doesn't match actual count | Error; file unchanged |
| After partial read, no `start_line`/`end_line` | Error requiring scoped params |
| Scoped range outside partial read range | Error |

## Matching

- UTF-16 LE detected via BOM.
- Fuzzy match via quote normalization (`findActualString`).
- `preserveQuoteStyle` in `new_string` when applicable.
- Atomic check-then-write (no async gap between staleness check and write).
- Scoped edit uses line-by-line streaming I/O: only the scoped line range is buffered in memory; before/after sections stream directly through.

## Post-Edit

Same as Write: update readFileState, LSP, file history, skill discovery.

## Edge Cases

| Case | Expected behavior |
|------|-------------------|
| Zero matches | Clear error with snippet hint |
| Overlapping matches | replace_all replaces all non-overlapping |
| Line ending mismatch | Normalize for match; write LF |
| Scoped edit on partial read, range within | Succeeds; only scoped range modified |
| Scoped edit on partial read, range outside | Error: outside read range |
| Scoped edit on partial read, no line params | Error: requires start_line/end_line |
| num_expected mismatch | Error; file unchanged (no partial write) |
| end_line < start_line | Error |

## Acceptance Criteria

- **AC1:** Read required; partial read rejected.
- **AC2:** Stale mtime rejected.
- **AC3:** old===new rejected.
- **AC4:** Multiple matches require replace_all.
- **AC5:** ipynb redirected to notebook tool.
- **AC6:** Scoped edit after partial read works within range.
- **AC7:** Scoped edit uses streaming I/O (before/after sections not buffered in memory).
- **AC8:** num_expected aborts on count mismatch.
- **AC9:** end_line < start_line rejected.
