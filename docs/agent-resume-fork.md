---
title: Agent Resume and Fork
slug: agent-resume-fork
priority: P4
status: not_started
spec: complete
code: not_started
package: internal/agent
gaps:
  []
depends_on:
  - task-subagent
---
# Agent Resume and Fork

## Overview

Agent tool (wire name `Agent`, legacy alias `Task`) spawns subagents with sync/async, fork, worktree isolation, and resume.

## Fork

- Omitting `subagent_type` when fork experiment enabled.
- Block recursive fork via `isInForkChild` (fork marker in history).
- Inherit parent system prompt bytes for cache identity.
- Placeholder tool_results for fork continuity.

## Worktree Isolation

`isolation: worktree` creates temp worktree.

Mutually exclusive with `cwd` override.

## Async

- `run_in_background` or agent `background: true`
- Returns `outputFile` path
- Auto-background after ~120s when configured
- Partial result on interrupt
- Resume restores worktree cwd from metadata

## Acceptance Criteria

- **AC1:** Recursive fork blocked.
- **AC2:** worktree isolation exclusive with cwd override.
- **AC3:** Async returns outputFile path.
- **AC4:** Interrupt yields partial result.
- **AC5:** Resume restores worktree state.
