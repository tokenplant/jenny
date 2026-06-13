---
title: Query Engine Lifecycle
slug: query-engine
priority: P1
status: done
spec: complete
code: done
package: internal/agent
gaps:
  - readFileState round-trip → P3 (requires Read tool state cache)
  - maxBudgetUsd via StreamConfig but not via QueryEngine method → P3
  - Cross-turn state (permissionDenials, discoveredSkillNames, etc.) → P3
defer_to: P3
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
| `maxIterations` | Maximum raw loop iterations (0 = unlimited); bounds API calls when set |
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
| maxIterations = 0 | Unlimited loop iterations (default) |
| maxIterations = 5 | Stop after 5 raw loop iterations |
| Structured output invalid schema | Error at tool registration |

## File structure

`internal/agent/` splits the engine across five files to keep each under ~800 lines:

| File | Responsibility |
|------|---------------|
| `engine.go` | Constructor, compaction counters, WireReadFileCache, SetMaxTurns |
| `engine_loop.go` | SubmitMessage, runLoop (skeleton with calls to extracted helpers) |
| `engine_stream.go` | emitConsolidatedAssistant, finalizeAsEndTurn, TurnCount, Model, Drain |
| `engine_results.go` | executeAndProcessTools, handleStreamError — tool execution, result NDJSON emission, streaming error handling |
| `engine_stopreasons.go` | handleStopReason — stop reason switch for end_turn, tool_use, max_tokens, stop_seq, default |

When splitting files, each needs its own import block; expect total line count to grow by up to ±30 lines over the pre-split single file due to per-file import headers.

## Acceptance Criteria

- **AC1:** User message on disk before API call starts.
- **AC2:** readFileState round-trips through ask().
- **AC3:** maxTurns enforced with distinct error subtype.
- **AC4:** maxBudgetUsd stops before next API call.
- **AC5:** Structured output requires schema + output tool.
