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

## Prompt Caching Design

Anthropic prompt caching (`prompt-caching-2024-07-31`) relies on byte-for-byte stability of the request prefix. Two mechanisms protect cache hits:

### System Prompt Freeze (Process-Level)

The assembled system prompt is **frozen on first call** and cached as `StreamConfig.CachedSystemPrompt`. Subsequent calls to `AssembleSystemPrompt` within the same process return the frozen string verbatim — regardless of git status changes, date changes, or memory content updates across turns.

Implementation: `internal/agent/engine_loop.go:120-122` captures the result from first `AssembleSystemPrompt` into `e.streamCfg.CachedSystemPrompt`.

### Cache-Control Split (Per-Request)

The stable and dynamic sections are sent as **two system blocks** to Anthropic:

| Block | Content | CacheControl | Section |
|-------|---------|-------------|---------|
| 1 (stable) | Default intro + memory content + tool list + skills manifest + redact instruction + append prompt | `ephemeral` | `buildSystemPrompt()` (via `provider_anthropic.go`) |
| 2 (dynamic) | Git status + platform/cwd | none | `DynamicSystemSuffix()` |

The stable block (typically 1000+ tokens) is cacheable and rarely changes. The dynamic suffix (<200 tokens) is re-sent every turn but too small to warrant a cache entry.

Rationale: sending git status and platform as a separate uncached suffix avoids busting the 1000+ token stable prefix every time the working tree changes.

### Resume Persistence (Cross-Process)

On first assembly, the frozen system prompt is persisted to the transcript as a `state` entry with field `system_prompt`. On session resume with `-r`, the `CachedSystemPrompt` is restored from this entry — ensuring the same system prompt bytes are sent across process boundaries.

Implementation: `session.Manager.AppendSystemPrompt()`, `session.Manager.LoadSystemPrompt()`, and `engine.go` restore logic.

## Assembly Flow

```
AssembleSystemPrompt(cfg, tools, cwd)
    │
    ├─ cfg.CachedSystemPrompt set?
    │     YES → return it (frozen, cache-friendly)
    │     NO  → buildSystemPrompt() → freeze into cfg.CachedSystemPrompt
    │           → persist to transcript
    │
    └─ API call: [block1(cache_control)] + [block2(no cache_control)]
```

## Default Sections

Static blocks: intro, system identity, doing tasks, actions, tone, using tools.

Dynamic registry sections (when enabled): memory, environment, MCP status, scratchpad, skills manifest.

## DynamicSystemSuffix

`DynamicSystemSuffix(cfg, cwd)` returns only the per-turn sections that should NOT be cached:

- **Git status** (branch, HEAD, status --short) — only if inside a git repo
- **Platform + cwd** (OS/arch, working directory path) — date is excluded (already in cached prefix)

Excluded from the suffix (these are in the cached system prompt block):
- Default intro
- Memory content (`<system-reminder>` block)
- Tool list
- Skills manifest
- Secret redaction instruction
- Append prompt

When `CustomSystemPrompt` is set, `DynamicSystemSuffix` returns empty string (custom replaces everything).

## Injected Context

| Section | Source | Limits |
|---------|--------|--------|
| User context | Project instruction files, date | Truncated per policy |
| System context | Git status snapshot (frozen at first assembly) | Max 2000 chars |
| Cwd | Current working directory | Absolute path |
| Platform | OS, arch | — |
| Date | time.Now() formatted as "YYYY-MM-DD" (frozen at first assembly) | — |

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
| Worktree cwd switch | Not supported across resume; new process re-freezes |
| MCP servers connect mid-session | Refresh MCP section next turn |
| `--bare` mode | Skip skills, memory, non-essential sections |
| Resume with different git status | Uses frozen prompt from transcript; suffix reflects new status |
| Cross-process same session ID | System prompt restored from transcript; cache may or may not hit depending on TTL |

## Acceptance Criteria

- **AC1:** Custom system prompt replaces defaults entirely.
- **AC2:** Tool list matches registered tools exactly.
- **AC3:** Git status injected with 2000 char cap.
- **AC4:** Deferred tools omitted from schemas when tool search on.
- **AC5:** appendSystemPrompt appended unless override suppresses.
- **AC6:** Default intro includes "autonomous" and "non-interactive" identity keywords
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
- **AC21:** Two calls to `AssembleSystemPrompt` with same cfg in same process return identical strings.
- **AC22:** `DynamicSystemSuffix` does not contain default intro, memory content, tool list, or any cached-section content.
- **AC23:** Anthropic API request body.System has two blocks: first with `cache_control`, second without.
- **AC24:** Resume restores `CachedSystemPrompt` from transcript; system prompt does not change.
- **AC25:** `--print-system-prompt` output ends with a newline character to prevent shell prompt overlap.
