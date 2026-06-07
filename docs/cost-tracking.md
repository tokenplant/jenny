---
title: Cost and Token Tracking
slug: cost-tracking
priority: P0
status: done
spec: complete
code: done
package: internal/agent, internal/session
gaps: []
defer_to: P3
depends_on:
  - anthropic-api-client
  - stream-json
---
# Cost and Token Tracking

## Overview

Jenny tracks per-model token usage and estimated USD cost across a session. Counters persist to project config and restore on resume when session IDs match.

## Tracked Fields

Per model (`ModelUsage`):

| Field | Description |
|-------|-------------|
| `inputTokens` | Uncached input tokens |
| `outputTokens` | Output tokens |
| `cacheReadInputTokens` | Tokens read from prompt cache |
| `cacheCreationInputTokens` | Tokens written to prompt cache |
| `webSearchRequests` | Server-side web search invocations |
| `costUSD` | Estimated cost for this model |

Session totals aggregate across models plus duration metrics (`totalAPIDuration`, `totalToolDuration`).

## Persistence

After each turn (or on shutdown), save to project config:

```json
{
  "lastSessionId": "sess_…",
  "lastModelUsage": { "claude-sonnet-4-…": { "inputTokens": 1000, … } },
  "totalCostUSD": 0.042
}
```

## Restore on Resume

`restoreCostStateForSession(sessionId)`:

- Restore counters **only if** `lastSessionId === sessionId`.
- Mismatch → reset to zero (prevents attributing prior session spend to new ID).

## Stream-JSON Terminal Line

Every successful headless run ends with a `result` line. Usage object uses **snake_case**:

```json
{
  "type": "result",
  "subtype": "success",
  "result": "…",
  "session_id": "sess_…",
  "usage": {
    "input_tokens": 1000,
    "output_tokens": 200,
    "cache_read_input_tokens": 500,
    "cache_creation_input_tokens": 100
  },
  "total_cost_usd": 0.012,
  "duration_ms": 4500,
  "duration_api_ms": 3200,
  "num_turns": 3,
  "stop_reason": "end_turn"
}
```

**Compatibility note:** Field names are `cache_read_input_tokens` and `cache_creation_input_tokens` (not `cache_read_tokens` / `cache_write_tokens`).

Additional optional fields: `modelUsage` (per-model breakdown), `model`.

## Budget Limits

When `maxBudgetUsd` is set on QueryEngine:

- Accumulate `totalCostUSD` each turn.
- Stop loop with budget-exceeded error before next API call when over limit.

## Edge Cases

| Case | Expected behavior |
|------|-------------------|
| Unknown model in pricing table | Set `hasUnknownModelCost`; still track tokens |
| Retry after 429 | Count tokens from successful attempt only (or sum per policy) |
| Compaction turn | Compaction agent usage counted separately or excluded per config |
| Resume with wrong ID | Zero restored cost |
| Fast mode / advisor usage | Merge advisor token counts if enabled |

## Headless Protocol Compatibility

- Terminal `result` line always present on success (stream-json).
- Usage fields must match snake_case schema expected by SDK consumers.
- `session_id` on result matches init line.

## Acceptance Criteria

- **AC1:** All four token types tracked per model when API returns them.
- **AC2:** Cost persists to project config with `lastSessionId`.
- **AC3:** Resume restores cost only on ID match.
- **AC4:** Stream-json `result.usage` includes cache token fields.
- **AC5:** `maxBudgetUsd` stops loop before exceeding budget.
