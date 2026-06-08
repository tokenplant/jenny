---
title: TaskStop Tool
slug: task-stop
priority: P4
status: done
spec: complete
code: done
package: internal/tool
gaps:
  []
depends_on:
  - background-tasks
---
# TaskStop Tool (Todo v2)

## Overview

Stops running background tasks.

## Rules

- Only `running` tasks stoppable
- Accepts deprecated `shell_id` alias for task ID

## Acceptance Criteria

- **AC1:** Non-running task errors clearly.
- **AC2:** shell_id alias accepted.
