---
title: TaskUpdate Tool
slug: task-update
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
# TaskUpdate Tool (Todo v2)

## Overview

Updates task fields, status, and dependency graph.

## Features

- `deleted` status support
- `addBlocks` / `addBlockedBy` for dependencies
- Metadata merge: null value deletes key
- Optional verification nudge

## Acceptance Criteria

- **AC1:** deleted status removes or marks task deleted.
- **AC2:** addBlocks/addBlockedBy update graph.
- **AC3:** null metadata key deletes field.
