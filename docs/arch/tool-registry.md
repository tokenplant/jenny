---
title: Tool Registry and Presets
slug: tool-registry
priority: P1
status: done
spec: complete
code: done
package: internal/tool
gaps: []
depends_on:
  - agent-loop
---
# Tool Registry and Presets

## Overview

Jenny registers tools from a canonical base list, filtered by feature flags and permission deny rules before the model sees schemas.

## Registration Flow

```
getAllBaseTools()
    → filter isEnabled()
    → filterToolsByDenyRules()
    → assembleToolPool() — built-ins first, then MCP, dedupe by name
```

Built-ins sorted first for prompt cache stability.

## Deny Rules

`filterToolsByDenyRules`:

- Blanket deny (no `ruleContent`) removes entire tool.
- MCP prefix rules supported (`mcp__server__*`).
- Denied tools never appear in API tool list.

## Todo v2 vs TodoWrite

When Todo v2 enabled:

- Register: TaskCreate, TaskGet, TaskUpdate, TaskList, TaskStop, TaskOutput.
- `TodoWriteTool.isEnabled()` returns false.

## Feature Flags

| Tool / group | Flag |
|--------------|------|
| LSP | `ENABLE_LSP_TOOL` env |
| EnterWorktree / ExitWorktree | `isWorktreeModeEnabled()` |
| Glob / Grep | Omitted when `hasEmbeddedSearchTools()` (embedded bfs/ugrep in shell) |
| Sleep | Not in default headless preset |

## MCP Tools

Merged after built-ins; names prefixed `mcp__<server>__<tool>`.

## Edge Cases

| Case | Expected behavior |
|------|-------------------|
| Duplicate tool name | Dedupe; built-in wins over MCP |
| Tool disabled mid-session | Removed on next turn assembly |
| Structured output mode | Inject synthetic StructuredOutput tool |

## Acceptance Criteria

- **AC1:** Denied tools absent from model tool list.
- **AC2:** Todo v2 disables TodoWrite.
- **AC3:** LSP only when flag enabled.
- **AC4:** MCP tools appended with correct prefix.
- **AC5:** Tool list in system prompt matches registered set.
