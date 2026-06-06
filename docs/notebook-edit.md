---
title: Notebook Edit Tool
slug: notebook-edit
priority: P2
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
# Notebook Edit Tool

## Overview

Modifies Jupyter `.ipynb` files only. Modes: replace, insert, delete. Read-before-write via readFileState.

## Parameters

| Param | Required | Description |
|-------|----------|-------------|
| `notebook_path` | yes | Absolute or relative `.ipynb` |
| `edit_mode` | no | `replace` (default), `insert`, `delete` |
| `cell_id` | conditional | Required for replace/delete; optional for insert-at-beginning |
| `cell_type` | insert only | `code` or `markdown` |
| `new_source` | replace/insert | Cell source string |

## Validation

- Extension must be `.ipynb` else error → use file Edit tool.
- readFileState must contain path (Read first).
- mtime > readTimestamp → stale error.
- Invalid JSON → error.
- Missing cell → error; supports `cell-N` numeric index alias.

## Execution

| Mode | Behavior |
|------|----------|
| replace | Set source; reset execution_count/outputs for code cells |
| insert | Splice after target or index 0; assign random id nbformat ≥4.5 |
| delete | Splice out cell |

Auto-convert replace→insert when replacing past end (default cell_type code).

Write JSON indent=1; preserve encoding/line endings from read metadata.

Update readFileState (offset undefined to break Read dedup).

## Edge Cases

| Case | Expected behavior |
|------|-------------------|
| UNC paths | Skip pre-validation I/O |
| Empty notebook insert | No cell_id → insert at beginning |
| In-place JSON mutation | Non-memoized parse in call() |

## Acceptance Criteria

- **AC1:** Non-ipynb rejected.
- **AC2:** Insert requires cell_type.
- **AC3:** Read + staleness enforced.
- **AC4:** Valid JSON after edit.
- **AC5:** Post-edit Read not file_unchanged stub.
