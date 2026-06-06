---
title: TaskCreate Tool
slug: task-create
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
# TaskCreate Tool (Todo v2)

## Overview

Creates tracked tasks in Todo v2 system. Requires Todo v2 enabled.

## Parameters

subject, description, optional activeForm, metadata.

## Hooks

Hooks may block creation and roll back (delete task) on failure.

## Acceptance Criteria

- **AC1:** Only enabled with Todo v2.
- **AC2:** Hook failure rolls back task.
