---
title: Skills Framework
slug: skills-framework
priority: P4
status: not_started
spec: complete
code: not_started
package: internal/skills
gaps:
  []
depends_on:
  - read
  - write
  - edit
---
# Skills Framework

## Overview

Discovers and activates skills from project and user directories on file tool operations.

## Discovery Triggers

On Read, Write, Edit path access:

- `discoverSkillDirsForPaths`
- `activateConditionalSkillsForPaths`

## Sources

- Project `project skills directory (e.g. .jenny/skills)` (or configured project skills dir)
- User config skills dir
- Bundled skills

Conditional skills activate on path glob match.

## Restrictions

- MCP prompts ≠ skills
- Plugin skills supported when installed
- `--bare` mode skips discovery

## Acceptance Criteria

- **AC1:** Skills discovered on Read/Write/Edit paths.
- **AC2:** Conditional activation on glob match.
- **AC3:** MCP prompts not invokable as skills.
- **AC4:** Bare mode skips discovery.
