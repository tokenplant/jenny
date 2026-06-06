---
title: TaskGet Tool
slug: task-get
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
# TaskGet Tool (Todo v2)

## Overview

Retrieves single task by ID.

## Behavior

Return `task: null` gracefully → tool result "Task not found" (not hard error).

## Acceptance Criteria

- **AC1:** Missing task returns null gracefully.
- **AC2:** Found task returns full record.
