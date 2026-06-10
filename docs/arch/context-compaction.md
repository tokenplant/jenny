---
title: Context Compaction
slug: context-compaction
priority: P2
status: done
spec: complete
code: done
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

## Model Parameters

Thresholds are derived from per-model parameters via `api.ModelParams(model)`:

| Model | Context Window | Max Output Tokens |
|-------|---------------|-------------------|
| `deepseek-v4-flash` | 1,000,000 | 8,192 |
| `deepseek-v4-pro` | 1,000,000 | 8,192 |
| Default (other models) | 200,000 | 20,000 |

`newCompactConfigForModel(model)` looks up actual values; `AUTO_COMPACT_WINDOW` env overrides context window if set.

## Threshold Math

```
effectiveContextWindow = modelContextWindow - modelMaxOutputTokens
autoCompactThreshold   = effectiveContextWindow - AUTOCOMPACT_BUFFER_TOKENS
warningThreshold       = autoCompactThreshold - WARNING_BUFFER_TOKENS
blockingLimit          = effectiveContextWindow - BLOCKING_BUFFER_TOKENS  (when auto-compact off)
```

Buffer constants scale with model output cap:

```
AUTOCOMPACT_BUFFER_TOKENS = max(modelMaxOutputTokens + 5_000, 13_000)
```

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

## Token Estimation

`estimateTokens` uses charset-aware heuristic:

- ASCII/Latin bytes: ~4 chars per token.
- Multi-byte (CJK, emoji, etc.): ~1.5 chars per token.

Target accuracy: within 10% for English, within 30% for CJK-heavy content.

## Acceptance Criteria

- **AC1:** Auto-compact at effectiveWindow − buffer.
- **AC2:** Circuit breaker after 3 failures.
- **AC3:** Hard block at effectiveWindow − 3K when auto off.
- **AC4:** Post-compact payload passes tool/thinking pairing rules.
- **AC5:** compact/session_memory sources never hard-blocked pre-API.
- **AC6:** Thresholds derived from actual model context window and max output tokens via `api.ModelParams`.
- **AC7:** `buildCompactedChain` does not split tool_use/tool_result pairs at boundary.

## Error Reporting: `stop_reason: max_tokens` {#error-reporting-stop_reason-max_tokens}

When the streaming API returns `stop_reason: "max_tokens"`, the engine emits a structured `result` event with `subtype: "error_max_tokens"` to distinguish between two failure categories:

### Categories

| Category | Condition | Fields Emitted |
|----------|-----------|----------------|
| `output_cap_hit` | `output_tokens >= modelMaxOutputTokens` | `category`, `model`, `output_tokens`, `max_output_tokens` |
| `context_exhausted` | `output_tokens < modelMaxOutputTokens` (request limited/rejected) | `category`, `model`, `input_tokens`, `threshold` |

### Structured `result` Event Schema

```json
{
  "type": "result",
  "subtype": "error_max_tokens",
  "result": "max tokens reached: <category>",
  "model": "<model_name>",
  "usage": {
    "input_tokens": <int>,
    "output_tokens": <int>,
    "cache_read_input_tokens": <int>,
    "cache_creation_input_tokens": <int>
  },
  "error_max_tokens": {
    "category": "<output_cap_hit|context_exhausted>",
    "output_tokens": <int>,        // only for output_cap_hit
    "max_output_tokens": <int>,   // only for output_cap_hit
    "input_tokens": <int>,        // only for context_exhausted
    "threshold": <int>             // only for context_exhausted (autoCompactThreshold)
  },
  "stop_reason": "max_tokens",
  "duration_ms": <int>,
  "total_cost_usd": <float>,
  "total_cost_cny": <float>
}
```

### Category Selection Rule

The categorizer in `internal/api/client.go` applies this logic:

1. **output_cap_hit** — The streaming response completed successfully with `stop_reason: "max_tokens"` and `output_tokens >= modelMaxOutputTokens`. The model hit its per-response output cap mid-generation.

2. **context_exhausted** — The streaming response completed with `stop_reason: "max_tokens"` but `output_tokens < modelMaxOutputTokens`. The request was rejected or severely limited before the model could generate significant output. This indicates the input context was too large for the model to generate meaningful response within its output cap.

### Model-Specific Max Output Tokens

| Model | Max Output Tokens |
|-------|-------------------|
| `deepseek-v4-flash` | 8,192 |
| `deepseek-v4-pro` | 8,192 |
| Default (other models) | 20,000 |

### Backward Compatibility

The new `error_max_tokens` subtype is **additive** to the stream-json output. Existing event subtypes (`success`, `error`, etc.) are unchanged. Consumers should ignore unknown subtypes to maintain forward compatibility with future event types.
