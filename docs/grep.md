---
title: Grep Tool
slug: grep
priority: P1
status: not_started
spec: complete
code: not_started
package: internal/tool
gaps:
  []
depends_on:
  - tool-registry
---
# Grep Tool

## Overview

Grep searches file contents via ripgrep. Read-only, concurrency-safe.

## Parameters

| Param | Default | Description |
|-------|---------|-------------|
| `pattern` | required | Regex pattern |
| `path` | cwd | Search path |
| `glob` | — | File filter |
| `output_mode` | `files_with_matches` | `content`, `files_with_matches`, `count` |
| `head_limit` | 250 | Max matches; `0` = unlimited |
| `offset` | 0 | Skip first N matches (pagination) |
| `-i`, `-n`, `-A`, `-B`, `-C` | — | Ripgrep passthrough |
| `multiline` | false | Multiline mode |
| `type` | — | File type filter |

## Ripgrep Invocation

- `--hidden` enabled.
- Auto-exclude VCS dirs (`.git`, `.svn`, etc.).
- `--max-columns 500` to avoid base64 line bloat.
- Pattern starting with `-`: use `-e` flag.

## Output Limits

- Total output capped ~**20K characters** (`maxResultSizeChars`).
- Ripgrep **timeout → error** (not empty result).
- Default mode `files_with_matches`; sort by mtime.

## Pagination

`offset` + `head_limit` apply across all output modes.

## Sandbox

When sandbox enabled, use configured sandboxed ripgrep path (see sandbox.md).

## Edge Cases

| Case | Expected behavior |
|------|-------------------|
| No matches | Empty result with clear message |
| Binary files | Skipped by ripgrep |
| Invalid regex | Error from ripgrep surfaced |
| Huge match count | head_limit + output cap |

## Acceptance Criteria

- **AC1:** Default head_limit 250; 0 unlimited.
- **AC2:** Pattern `-foo` uses `-e`.
- **AC3:** Timeout returns error.
- **AC4:** Output capped at ~20K chars.
- **AC5:** VCS dirs excluded.
