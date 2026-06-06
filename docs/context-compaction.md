---
title: Context Compaction
slug: context-compaction
priority: P2
status: not_started
spec: complete
code: not_started
package: internal/agent
gaps:
  []
depends_on:
  - message-normalization
  - query-engine
---
# Context Compaction

## Overview

Automatic and manual compaction summarizes older turns to fit within the model context window. Inserts a compact boundary and rebuilds a shorter message chain.

## Threshold Math

```
effectiveContextWindow = modelContextWindow - min(modelMaxOutputTokens, 20_000)
autoCompactThreshold   = effectiveContextWindow - 13_000   (AUTOCOMPACT_BUFFER_TOKENS)
warningThreshold       = autoCompactThreshold - 20_000
blockingLimit          = effectiveContextWindow - 3_000     (when auto-compact off)
```

Optional env `AUTO_COMPACT_WINDOW` caps window used in calculation.

## Trigger

Auto-compact when estimated tokens ≥ `autoCompactThreshold`.

Subtract `snipTokensFreed` when snip already removed messages but usage metadata still reflects pre-snip size.

**Disabled when:** `DISABLE_COMPACT`, `DISABLE_AUTO_COMPACT`, user setting off, or `querySource` is `compact` / `session_memory`.

## Summary Reserve

Compaction summary call may consume up to **20K output tokens** (p99.99 summaries ≈ 17.4K).

## Circuit Breaker

Track `consecutiveFailures` per session.

- Failure (non-user-abort): increment.
- Success: reset to 0.
- After **3** failures (`MAX_CONSECUTIVE_AUTOCOMPACT_FAILURES`): skip all further auto-compact for session.

## Execution

1. Try session-memory compaction when enabled.
2. Else fork summary agent: `querySource=compact`, tools disabled, max 1 turn.
3. Strip images/documents from summarizer input (replace with `[image]` / `[document]` markers).
4. On prompt-too-long during summarize: retry up to 3× dropping oldest API-round groups from head.

Post-compact order: `boundaryMarker → summaryMessages → messagesToKeep → attachments → hookResults`.

## Post-Compact Normalization

Before next API call:

1. Filter orphaned thinking-only messages.
2. Strip trailing thinking from last assistant (insert `[No message content]` if needed).
3. `ensureToolResultPairing`.
4. Filter whitespace-only assistant messages.

## Warning and Hard Block

- Warning flags when tokens ≥ threshold − 20K.
- With auto-compact **off**: block API calls at `effectiveWindow - 3K` with prompt-too-long error (except compact/session_memory sources).

## Edge Cases

| Case | Expected behavior |
|------|-------------------|
| Re-compaction same chain | Track turnCounter for telemetry |
| Partial compact (from vs up_to) | Different cache invalidation |
| Task budget | Subtract pre-compact context from remaining budget |
| Blocking check after compact | Skip stale usage check once |

## Acceptance Criteria

- **AC1:** Auto-compact at effectiveWindow − 13K.
- **AC2:** Circuit breaker after 3 failures.
- **AC3:** Hard block at effectiveWindow − 3K when auto off.
- **AC4:** Post-compact payload passes tool/thinking pairing rules.
- **AC5:** compact/session_memory sources never hard-blocked pre-API.
