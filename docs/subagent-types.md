---
title: Subagent Types
slug: subagent-types
priority: P4
status: not_started
spec: complete
code: not_started
package: internal/agent
gaps:
  []
depends_on:
  - tool-registry
---
# Subagent Types

## Overview

Built-in subagent types with distinct tool allowlists, models, and resume semantics.

## Built-in Types

| Type | Tools | Notes |
|------|-------|-------|
| general-purpose | `*` (all allowed) | Default subagent |
| explore | Read, Glob, Grep, Bash (read-only) | Disallows Write, Edit, Agent |
| plan | Read-only subset | Planning tasks |
| shell | Bash-focused | Command execution |
| verification | Read + test runners | CI-style checks |

Each has `whenToUse` description, optional model (`inherit` or alias), `disallowedTools`, `omitProjectInstructions` for read-only agents.

## Filtering

Filter by permission deny rules and required MCP servers.

## Model Aliases

`sonnet`, `opus`, `haiku` → concrete models via resolver.

## One-Shot Types

Explore, Plan: cannot resume (`ONE_SHOT_BUILTIN_AGENT_TYPES`).

## Acceptance Criteria

- **AC1:** Each type has distinct tool allowlist.
- **AC2:** Deny rules filter subagent types.
- **AC3:** Model aliases resolve correctly.
- **AC4:** One-shot types reject resume.
- **AC5:** MCP requirements enforced per type.
