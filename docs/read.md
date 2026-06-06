---
title: Read Tool
slug: read
priority: P1
status: partial
spec: complete
code: partial
package: internal/tool
gaps:
  - Naive line scan
  - No size/token limits
  - No images/PDF/notebooks
  - No file_unchanged dedup
  - No block device guard
depends_on:
  - tool-registry
---
# Read Tool

## Overview

Read returns file contents with line numbers, or structured blocks for images/PDFs/notebooks. Enforces size limits, read deduplication, and path security.

## Parameters

| Param | Description |
|-------|-------------|
| `file_path` | Absolute or relative path (expand `~`) |
| `offset` | 1-based start line; `0` treated as line 1 |
| `limit` | Max lines to return |
| `pages` | PDF page limit per request |

## Size Limits

| Limit | Default | When checked | On exceed |
|-------|---------|--------------|-----------|
| `maxSizeBytes` | 256 KB | stat before read | Throw pre-read |
| `maxTokens` | 25,000 | after read | Throw post-read (not silent truncate) |

Partial reads (`offset`/`limit`): use range read — do not load full file.

## Binary Files

Extension blocklist rejects binary files. **Exempt:** images (png, jpg, gif, webp), PDFs.

## Images

Resize/compress to token budget; return image content block with dimension metadata.

## PDFs

- Small + model supports: inline document block.
- Large: extract pages to JPEGs; `pages` limits pages per request.
- Poppler fallback when native extraction fails.

## Notebooks (`.ipynb`)

Parse cells as structured content. When oversized, suggest Bash/`jq` approach in error.

## Dedup (`file_unchanged`)

Same path + offset + limit + mtime unchanged since last read → return stub indicating file unchanged.

**Not applied:** after Write/Edit cache entries, partial views, or when offset/limit differ.

## Block Devices

Reject without reading: `/dev/zero`, `/dev/urandom`, stdio fds, `/proc/self/fd/{0,1,2}`.

## macOS Screenshots

Filenames with thin-space (U+202F) before AM/PM: retry alternate space variant on ENOENT.

## Path Security

- Expand `~`; resolve relative to cwd.
- Deny-rule matching before I/O.
- ENOENT: suggest similar files.
- UNC paths (Windows): skip pre-validation stat (NTLM leak prevention).

## Side Effects

- Skill directory discovery on read paths.
- File-read listeners (LSP, history).
- Auto-memory freshness prefix when reading memory files (see memdir.md).

## Output Format

Line-numbered text:

- Compact: `{line}\t{content}`
- Legacy: `{line padded 6 chars}→{content}`

Empty file or offset past EOF: warning in result, not hard error.

## Edge Cases

| Case | Expected behavior |
|------|-------------------|
| Symlink outside cwd | Reject per path policy |
| UTF-16 LE with BOM | Detect encoding |
| File changes during read | mtime check on subsequent Write/Edit |
| 256KB file, limit 10 lines | Byte gate on total file size still applies |

## Acceptance Criteria

- **AC1:** Files > 256KB rejected before read.
- **AC2:** Output > 25K tokens rejected after read.
- **AC3:** offset=0 reads from line 1.
- **AC4:** Unchanged file returns file_unchanged stub.
- **AC5:** Block device paths rejected without read.
