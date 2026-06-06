---
title: Edit Tool
slug: edit
priority: P1
status: not_started
spec: complete
code: not_started
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

## Parameters

| Param | Description |
|-------|-------------|
| `file_path` | Target path |
| `old_string` | Text to find |
| `new_string` | Replacement text |
| `replace_all` | Replace all occurrences (required when multiple matches) |

## Limits

- Max file size: **1 GiB** (stat bytes).
- Partial read blocks edit (same as Write).

## Validation

| Condition | Result |
|-----------|--------|
| `old_string === new_string` | Rejected |
| `old_string === ''` on missing file | Create file |
| `old_string === ''` on non-empty file | Error |
| Multiple matches without `replace_all` | Error requiring replace_all |
| `.ipynb` path | Redirect to NotebookEdit tool |

## Matching

- UTF-16 LE detected via BOM.
- Fuzzy match via quote normalization (`findActualString`).
- `preserveQuoteStyle` in `new_string` when applicable.
- Atomic check-then-write (no async gap between staleness check and write).

## Post-Edit

Same as Write: update readFileState, LSP, file history, skill discovery.

## Edge Cases

| Case | Expected behavior |
|------|-------------------|
| Zero matches | Clear error with snippet hint |
| Overlapping matches | replace_all replaces all non-overlapping |
| Line ending mismatch | Normalize for match; write LF |

## Acceptance Criteria

- **AC1:** Read required; partial read rejected.
- **AC2:** Stale mtime rejected.
- **AC3:** old===new rejected.
- **AC4:** Multiple matches require replace_all.
- **AC5:** ipynb redirected to notebook tool.
