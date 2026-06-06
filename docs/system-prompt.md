---
title: System Prompt Assembly
slug: system-prompt
priority: P1
status: partial
spec: complete
code: partial
package: internal/agent
gaps:
  - Static one-line default only
  - No cwd/git/platform sections
  - No custom replace-default semantics
depends_on:
  - git-helpers
  - tool-registry
---
# System Prompt Assembly

## Overview

Jenny assembles the model system prompt from static sections, dynamic context (cwd, git, tools), and optional user overrides. Custom system prompt **replaces** the default entirely — it is not appended to defaults.

## Assembly Flow

```
fetchSystemPromptParts()
    │
    ├─ customSystemPrompt set?
    │     YES → defaultSystemPrompt = []; use custom + append only
    │     NO  → load default sections + getSystemContext()
    │
    ├─ resolveSystemPromptSections() — registry blocks
    │
    └─ Final: [custom OR defaults] + memory mechanics + appendSystemPrompt
```

## Default Sections

Static blocks: intro, system identity, doing tasks, actions, tone, using tools.

Dynamic registry sections (when enabled): memory, environment, MCP status, scratchpad, skills manifest.

## Injected Context

| Section | Source | Limits |
|---------|--------|--------|
| User context | Project instruction files, date | Truncated per policy |
| System context | Git status snapshot | Max 2000 chars |
| Cwd | Current working directory | Absolute path |
| Platform | OS, arch | — |

Git status truncated at 2000 characters with ellipsis.

## Tool List Sync

`getUsingYourToolsSection(enabledTools)` built from **actually registered** tool names at runtime.

Must mention available tools (Read, Edit, Write, Glob, Grep, Bash) or embedded-search variants when Glob/Grep omitted.

When tool search enabled: omit deferred tool **descriptions** from API schemas (`deferLoading: true`); optional meta message lists deferred tool names.

## Override Rules

| Input | Effect |
|-------|--------|
| `customSystemPrompt` | Replaces all default sections |
| `appendSystemPrompt` | Appended after custom or defaults (unless `overrideSystemPrompt` set) |
| `overrideSystemPrompt` | Suppresses append |

## Edge Cases

| Case | Expected behavior |
|------|-------------------|
| Empty custom prompt | Valid but minimal; still inject append if set |
| Worktree cwd switch | Rebuild git + memory sections; clear caches |
| MCP servers connect mid-session | Refresh MCP section next turn |
| `--bare` mode | Skip skills, memory, non-essential sections |

## Acceptance Criteria

- **AC1:** Custom system prompt replaces defaults entirely.
- **AC2:** Tool list matches registered tools exactly.
- **AC3:** Git status injected with 2000 char cap.
- **AC4:** Deferred tools omitted from schemas when tool search on.
- **AC5:** appendSystemPrompt appended unless override suppresses.
