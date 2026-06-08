---
title: Swarm (Parallel Agents)
slug: swarm
priority: P4
status: done
spec: complete
code: done
package: internal/agent
gaps:
  []
depends_on:
  - task-subagent
---
# Swarm (Parallel Agents)

## Overview

Flat roster of parallel agents — no nested teammates. Team coordination gated behind feature flag.

## Rules

- Flat roster only — teammate cannot spawn nested teammate with `name`.
- `isAgentSwarmsEnabled` gates team tools together.
- Teammate spawn via Agent tool `name` param.
- In-process vs tmux backends supported upstream; headless Jenny may use in-process only.

## Out of Scope (Headless v1)

SendMessage, team delete, coordinator messaging.

## Acceptance Criteria

- **AC1:** No nested named teammates.
- **AC2:** Swarm feature flag gates all team tools.
- **AC3:** Flat delegation only in headless mode.
