---
title: Sleep and Await
slug: sleep-await
priority: P3
status: done
spec: complete
code: done
package: internal/tool
gaps:
  []
depends_on:
  - background-tasks
  - task-output
---
# Sleep and Await

## Overview

Headless Jenny has no standalone Sleep tool in default preset. Waiting uses TaskOutput or Read on background output files.

## TaskOutput Blocking

| Param | Default |
|-------|---------|
| `block` | true |
| Poll interval | 100ms |
| timeout | 30s (max 600s) |

Prefer in-memory agent result over raw transcript JSONL.

## Bash Sleep Block

Block standalone `sleep` ≥ **2** seconds in Bash — use TaskOutput with block=true instead.

## Background Bash

`run_in_background` spawns tracked task; progress hint after ~2s.

Disallow sleep for auto-background promotion.

## Acceptance Criteria

- **AC1:** No Sleep in default tool preset.
- **AC2:** Bash sleep ≥2 blocked.
- **AC3:** TaskOutput block polls at 100ms.
- **AC4:** Default timeout 30s, max 600s.
- **AC5:** In-memory result preferred over JSONL.
