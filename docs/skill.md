---
title: Skill Tool
slug: skill
priority: P3
status: not_started
spec: complete
code: not_started
package: internal/tool
gaps:
  []
depends_on:
  - tool-registry
---
# Skill Tool

## Overview

Invokes slash-command skills by name. Permission check on name and content.

## Behavior

- Load skill definition from project/user/bundled dirs.
- Heavy skills may fork into isolated sub-agent with own token budget.
- MCP **prompts** not invokable — only MCP skills with proper loadedFrom marker.
- Cannot invoke via guessed mcp__ prompt names.

## Acceptance Criteria

- **AC1:** Permission check before execution.
- **AC2:** Heavy skills fork with separate budget.
- **AC3:** MCP prompts rejected.
- **AC4:** Unknown skill name errors clearly.
