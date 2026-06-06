---
title: TaskList Tool
slug: task-list
priority: P4
status: not_started
spec: complete
code: not_started
package: internal/tool
gaps:
  []
depends_on:
  - task-create
---
# TaskList Tool (Todo v2)

## Overview

Lists tasks with optional filters.

## Behavior

- Filter `_internal` metadata from output
- Strip resolved blockers from `blockedBy` arrays

## Acceptance Criteria

- **AC1:** Internal metadata not exposed.
- **AC2:** Resolved blockers stripped from blockedBy.
