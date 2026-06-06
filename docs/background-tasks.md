---
title: Background Tasks
slug: background-tasks
priority: P4
status: not_started
spec: complete
code: not_started
package: internal/agent
gaps:
  []
depends_on:
  - bash
---
# Background Tasks

## Overview

Long-running shell and agent tasks tracked with progress, output files, and parent notifications.

## Bash Background

- `run_in_background` spawns tracked shell task.
- Progress events after ~2s.
- Output: `.jenny/.../tasks/<id>.output`
- Disk cap 5GB; O_NOFOLLOW on Unix.

## Auto-Background

Promote long sync agents when configured. Disallow `sleep` for auto-background promotion.

## Completion

Structured notification XML to parent agent on task completion.

## TaskStop

Only `running` tasks; accepts deprecated `shell_id` alias.

## Acceptance Criteria

- **AC1:** Background bash writes to output file.
- **AC2:** Progress after 2s.
- **AC3:** Completion notifies parent.
- **AC4:** sleep disallowed for auto-background.
- **AC5:** TaskStop only for running tasks.
