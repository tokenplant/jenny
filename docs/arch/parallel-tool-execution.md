---
title: Parallel Tool Execution
slug: parallel-tool-execution
priority: P1
status: done
spec: complete
code: done
package: internal/agent
gaps:
  []
depends_on:
  - agent-loop
  - tool-registry
---
# Parallel Tool Execution

## Overview

The agent loop executes model-requested tools with concurrency for read-only tools and serialization for mutating or shell tools. Results are ordered by request sequence, not completion time.

## Execution Paths

1. **Streaming path** — `StreamingToolExecutor` runs tools while SSE response is still streaming (tool_use blocks arrive incrementally).
2. **Batch path** — `runTools` executes all tool_use blocks after full assistant message received.

Both paths share the same concurrency rules.

## Concurrency Rules

| Tool class | Execution |
|------------|-----------|
| `isConcurrencySafe(input) === true` | May run in parallel (default max 10, env override) |
| Write, Edit, Bash (mutating) | Serialized |
| Mixed batch | Consecutive safe tools parallel; stop at first unsafe tool until it completes |

Partitioning: group consecutive concurrency-safe tools into parallel batches; switch to serial for writes/bash.

## Bash Sibling Abort

When a **bash** tool fails (non-zero exit or execution error):

- Set batch error flag.
- Abort sibling subprocesses via `AbortController` with reason `sibling_error`.
- Other tool types failing do **not** abort siblings.

## Unknown Tool

Immediate synthetic error — never hang:

```
Error: No such tool available: {name}
```

Wrapped in tool error format; status `completed`.

## Result Ordering

`getCompletedResults()` yields in tool **add order** (order model requested), not completion order. Mark each tool `yielded` after emission.

## Progress Events

Messages with `type: progress` (bash progress, MCP progress):

- Stored in `pendingProgress`.
- Yield immediately to stream-json consumers.
- Separate from final `tool_result`.

## Context Modifiers

Applied only for **non-concurrent** tools after completion (e.g. cwd updates, file history).

## Streaming Fallback Discard

When SSE streaming fails and falls back to non-streaming:

- Call `discard()` on streaming executor.
- Abandon in-flight tool work with reason `Streaming fallback - tool execution discarded`.
- Do not reuse partial tool_use IDs.

## Edge Cases

| Case | Expected behavior |
|------|-------------------|
| 10+ parallel reads | Cap at max concurrency env |
| Bash + Read same turn | Bash serializes; reads may complete first if batched separately |
| Interrupt mid-batch | Synthetic errors for pending tools |
| Duplicate tool names same turn | Each tool_use ID independent |

## Acceptance Criteria

- **AC1:** Read/Glob/Grep run in parallel when consecutive.
- **AC2:** Write/Edit/Bash never run concurrently with each other or with parallel batch.
- **AC3:** Bash failure aborts sibling bash in same batch.
- **AC4:** Unknown tool returns immediate error.
- **AC5:** Results emitted in request order.
