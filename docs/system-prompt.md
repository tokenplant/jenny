---
title: System Prompt Assembly
slug: system-prompt
priority: P1
status: done
spec: complete
code: done
package: internal/agent
depends_on:
  - git-helpers (done)
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
| Date | time.Now() formatted as "YYYY-MM-DD" | — |

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

## Instruction File Loading

Jenny loads project-level instruction files from the working directory in priority order:

1. `<cwd>/CLAUDE.md` — if present, its content is injected as a `<system-reminder>` block at the top of the system prompt.
2. `<cwd>/AGENTS.md` — loaded only when `CLAUDE.md` is absent.

When neither file exists, no instruction block is injected. Subdirectory instruction files (e.g. `subdir/CLAUDE.md`) are NOT loaded — only the cwd-root file is read.

## Acceptance Criteria

- **AC1:** Custom system prompt replaces defaults entirely.
- **AC2:** Tool list matches registered tools exactly.
- **AC3:** Git status injected with 2000 char cap.
- **AC4:** Deferred tools omitted from schemas when tool search on.
- **AC5:** appendSystemPrompt appended unless override suppresses.
- **AC6:** Default intro opens with "You are an AI assistant"
- **AC7:** Assembled prompt >= 1000 chars with default tools and no git repo
- **AC8:** Default intro includes bash-safety language ("destructive" or "rm -rf")
- **AC9:** Default prompt names Glob and Grep as search tools
- **AC10:** No unfilled template placeholders in assembled output
- **AC11:** AC1–AC5 from iter-115 continue to pass (regression)
- **AC12:** `--print-system-prompt` stdout contains "date" or "Date" (date is injected into the platform/context section).
- **AC13:** The injected date reflects the current calendar year.
- **AC14:** `--print-system-prompt` stdout contains OS/platform info (`"Platform"`, `"darwin"`, `"linux"`, or `"windows"`).
- **AC15:** When jenny is launched from inside a git repository, `--print-system-prompt` stdout contains the current branch (substring `"Branch"` or `"Git context"`).
- **AC16:** When jenny is launched from a directory with no git repo, `--print-system-prompt` stdout does NOT contain `"Git context"` or `"Branch:"`.
- **AC17:** CLAUDE.md content from cwd appears in system prompt as a `<system-reminder>` block.
- **AC18:** AGENTS.md used as fallback when CLAUDE.md absent.
- **AC19:** CLAUDE.md takes precedence when both files exist.
- **AC20:** Subdirectory CLAUDE.md is not loaded.
