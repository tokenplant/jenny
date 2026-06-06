---
title: TodoWrite Tool
slug: todo-write
priority: P4
status: not_started
spec: complete
code: not_started
package: internal/tool
gaps:
  []
depends_on:
  - tool-registry
---
# TodoWrite Tool

## Overview

In-session todo list for agent planning. Stored per agent/session key.

## Behavior

- All items `completed` → store cleared to `[]`
- Disabled when Todo v2 enabled (TaskCreate/Get/List/Update replace it)

## Acceptance Criteria

- **AC1:** Todos persist per session key.
- **AC2:** All completed clears list.
- **AC3:** Disabled when Todo v2 on.
