---
title: Bash Tool
slug: bash
priority: P1
status: partial
spec: complete
code: partial
package: internal/tool
gaps:
  - Read-only prefix allowlist only
  - No sandbox/classifier/sed simulation
  - No output spill or run_in_background
depends_on:
  - tool-registry
  - dangerous-command-gate
---
# Bash Tool

## Overview

Bash executes shell commands with permission classifier, optional sandbox, read-only constraints, output limits, and background execution support.

## Parameters

| Param | Description |
|-------|-------------|
| `command` | Shell command string |
| `timeout` | Max execution time (ms) |
| `run_in_background` | Spawn tracked background task |
| `dangerouslyDisableSandbox` | Per-invocation sandbox opt-out (internal) |

## Permission Flow

```
bashToolHasPermission()
    → dangerous-command gate
    → read-only pipeline check (if read-only mode)
    → classifier (unless bypass permissions)
    → shouldUseSandbox() unless dangerouslyDisableSandbox
```

## Read-Only Mode

- Massive allowlist with flag-level validation.
- Pipelines: every segment must pass read-only check.
- `isConcurrencySafe` true only for read-only commands.

## Sandbox

Wrap command via sandbox backend when enabled (see sandbox.md).

## Sed Simulation

In-place `sed` edits may be simulated as file edits internally:

- Parse sed command → apply as Edit/Write.
- Never expose internal `_simulatedSedEdit` in tool schema.
- Track git attribution; notify on file changes.

## Output Limits

- Inline cap ~**30K characters**.
- Larger output spilled to disk; tool result references path.

## Timeout and Cwd

- Default/max timeout from tool config.
- After execution: `resetCwdIfOutsideProject` if cwd drifted outside project root.

## Background Execution

- `run_in_background`: spawn tracked shell task.
- Progress events after ~2s.
- Block standalone `sleep ≥2` seconds — use TaskOutput with block=true.
- Auto-background long sync agents when configured (~15s assistant mode).

## Exit Codes

Non-zero exit may receive semantic interpretation via `interpretCommandResult` (optional).

## Edge Cases

| Case | Expected behavior |
|------|-------------------|
| Bash fails in parallel batch | Abort sibling bash processes |
| Sandbox unavailable | Fail with clear reason if sandbox required |
| Output spill disk full | Error with partial path if any |
| Heredoc / substitution | Blocked by dangerous-command gate |

## Acceptance Criteria

- **AC1:** Read-only pipelines validated per segment.
- **AC2:** Output >30K spilled to disk.
- **AC3:** sleep ≥2 blocked in foreground bash.
- **AC4:** Cwd reset when outside project.
- **AC5:** Sed simulation invisible in schema.
