---
title: Task Tool (Subagent)
slug: task-subagent
priority: P4
status: not_started
spec: complete
code: not_started
package: internal/agent
gaps:
  []
depends_on:
  - subagent-types
  - agent-loop
---
# Task Tool (Subagent)

## Overview

Spawns subagents with typed tool allowlists. Wire tool name `Agent` (legacy alias `Task`).

## Parameters

| Param | Description |
|-------|-------------|
| `description` | Short task label |
| `prompt` | Subagent instruction |
| `subagent_type` | Built-in type name |
| `model` | Optional model override |
| `run_in_background` | Async execution |
| `isolation` | `worktree` for temp worktree |
| `cwd` | Working directory override |

## Results

| Mode | Return |
|------|--------|
| Sync | Final text result |
| Async | `{ status: async_launched, agentId, outputFile }` |

Per-type tools via `resolveAgentTools`. Partial extraction on interrupt.

## Acceptance Criteria

- **AC1:** subagent_type selects allowlist.
- **AC2:** Sync returns text; async returns outputFile.
- **AC3:** worktree isolation creates temp worktree.
- **AC4:** Interrupt extracts partial result.
