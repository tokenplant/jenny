---
title: Query Engine Lifecycle
slug: query-engine
priority: P1
status: not_started
spec: complete
code: not_started
package: internal/agent
gaps:
  - Run/RunStream only; no readFileState, maxTurns, maxBudgetUsd, structured output
depends_on:
  - session-persistence
  - agent-loop
---
# Query Engine Lifecycle

## Overview

QueryEngine orchestrates headless query lifecycle: persist user input, run agent loop, restore session state, enforce turn/budget limits.

## Lifecycle

```
submitMessage(prompt)
    → persist user message to transcript (before API)
    → clone readFileState from cache
    → run query loop
    → flush transcript + cost state
    → yield SDK messages (stream-json)
```

## Persist Before API

Record user message to transcript **before** first API call of turn.

Survives process kill mid-request; resume shows user prompt even if assistant never responded.

## readFileState

- Constructor accepts `readFileCache`.
- `ask()` clones via `cloneFileStateCache(getReadFileCache())`.
- `finally` block writes back via `setReadFileCache(engine.getReadFileState())`.
- Resume seeds cache from transcript (see session-resume.md).

## Cross-Turn State

Carried across turns in engine instance:

- `permissionDenials[]`
- `discoveredSkillNames`
- `loadedNestedMemoryPaths`
- File history updaters
- Git attribution state

## Limits

| Option | Behavior |
|--------|----------|
| `maxTurns` | Stop loop; emit `error_max_turns` result |
| `maxBudgetUsd` | Stop before API when cost exceeded |
| `jsonSchema` | Structured output; requires synthetic output tool + session hook |

## Structured Output

Requires JSON schema param plus StructuredOutput tool in pool.

Register enforcement hook on session ID; validate with schema at tool creation and invocation.

## Skills and Plugins

Loaded per turn; merge coordinator userContext when enabled.

## Edge Cases

| Case | Expected behavior |
|------|-------------------|
| Abort mid-turn | Synthetic tool_results; partial transcript persisted |
| Resume same engine | Rehydrate from transcript file |
| maxTurns = 1 | Single API iteration max |
| Structured output invalid schema | Error at tool registration |

## Acceptance Criteria

- **AC1:** User message on disk before API call starts.
- **AC2:** readFileState round-trips through ask().
- **AC3:** maxTurns enforced with distinct error subtype.
- **AC4:** maxBudgetUsd stops before next API call.
- **AC5:** Structured output requires schema + output tool.
