---
title: TaskOutput Tool
slug: task-output
priority: P4
status: done
spec: complete
code: done
package: internal/tool
gaps:
  []
depends_on:
  - background-tasks
  - task-subagent
---
# TaskOutput Tool (Todo v2)

## Overview

Retrieves output from background tasks/agents with optional blocking wait.

## Parameters

| Param | Default |
|-------|---------|
| `block` | true |
| Poll interval | 100ms |
| `timeout` | 30s (max 600s) |

Prefer in-memory agent result over raw transcript JSONL.

## Acceptance Criteria

- **AC1:** block=true polls every 100ms.
- **AC2:** Default timeout 30s.
- **AC3:** Max timeout 600s.
- **AC4:** In-memory result preferred.
