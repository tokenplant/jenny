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

Jenny tracks per-model token usage and estimated USD cost across a session. All cost tracking is USD-only (no CNY). Counters persist to project config and restore on resume when session IDs match.

## Tracked Fields

Per model (`ModelUsage`):

| Field | Description |
|-------|-------------|
| `inputTokens` | Uncached input tokens |
| `outputTokens` | Output tokens |
| `cacheReadInputTokens` | Tokens read from prompt cache |
| `cacheCreationInputTokens` | Tokens written to prompt cache |
| `costUSD` | Estimated USD cost for this model |

Session totals (`CostState`):

| Field | Description |
|-------|-------------|
| `totalCostUSD` | Sum of all model costs in USD |
| `hasUnknownModelCost` | Set when an unknown model is used |

## Pricing Table

`DefaultPricing` is a map of model name → `ModelPricing` with per-token USD rates. All entries include a `// source:` comment citing official provider pricing.

### Model Families and Official USD Rates (June 2026)

| Model Family | Input USD/MTok | Output USD/MTok | Cache Read USD/MTok | Cache Creation USD/MTok | Source |
|--------------|----------------|-----------------|---------------------|-------------------------|--------|
| Claude Sonnet 4.x (4, 4.5, 4.6) | $3.00 | $15.00 | $0.30 | $3.75 | claude.com/pricing#api |
| Claude Opus 4.x (4, 4.1, 4.5–4.8) | $5.00 | $25.00 | $0.50 | $3.75 | claude.com/pricing#api |
| Claude Haiku 4.5 | $1.00 | $5.00 | $0.10 | $0.30 | claude.com/pricing#api |
| Claude 3.5 Sonnet (legacy) | $3.00 | $15.00 | $3.00 | $3.75 | claude.com/pricing#api |
| Claude 3 Opus (legacy) | $15.00 | $75.00 | $15.00 | $3.75 | claude.com/pricing#api |
| DeepSeek V4 Flash | $0.14 | $0.28 | $0.0028 | — | api-docs.deepseek.com/quick_start/pricing |
| DeepSeek V4 Pro | $0.435 | $0.87 | $0.003625 | — | api-docs.deepseek.com/quick_start/pricing |
| Gemini 2.5 Flash | $0.30 | $2.50 | $0.03 | — | cloud.google.com/vertex-ai/generative-ai/pricing |
| Gemini 2.1 Pro | $1.25 | $10.00 | $0.125 | — | cloud.google.com/vertex-ai/generative-ai/pricing |
| MiniMax M3 | $0.58 | $2.33 | $0.12 | — | platform.minimaxi.com/docs/guides/pricing-paygo |
| MiniMax M2.7 / M2.7-highspeed | $0.30 / $0.58 | $1.17 / $2.33 | $0.06 | $0.08 | platform.minimaxi.com/docs/guides/pricing-paygo |
| MiniMax M2.5 | $0.29 | $1.17 | $0.03 | $0.07 | platform.minimaxi.com/docs/guides/pricing-paygo |
| Kimi K2.7 Code | $0.83 | $4.72 | $0.83 | — | platform.kimi.com/docs/pricing/chat-k26 |
| Kimi K2.6 | $0.63 | $3.61 | $0.63 | — | platform.kimi.com/docs/pricing/chat-k26 |
| Kimi K2.5 | $0.49 | $2.92 | $0.49 | — | platform.kimi.com/docs/pricing/chat-k25 |
| Moonshot V1-8k | $0.07 | $0.42 | $0.07 | — | platform.kimi.com/docs/pricing/chat-v1 |
| Moonshot V1-32k | $0.14 | $0.42 | $0.14 | — | platform.kimi.com/docs/pricing/chat-v1 |
| Moonshot V1-128k | $0.28 | $0.42 | $0.28 | — | platform.kimi.com/docs/pricing/chat-v1 |
| Qwen 3.7 Max | $2.50 | $7.50 | $0.25 | $3.125 | www.qwencloud.com/models/qwen3.7-max |
| Qwen 3.5-27B | $0.30 | $2.40 | — | — | www.qwencloud.com/models/qwen3.5-27b |
| Qwen Turbo | $0.10 | $0.60 | — | — | alibabacloud.com |
| Hunyuan Turbos | $0.11 | $0.28 | — | — | cloud.tencent.com/document/product/1729/97731 |
| Hunyuan T1 | $0.14 | $0.56 | — | — | cloud.tencent.com/document/product/1729/97731 |
| Hunyuan Hy-2.0 Instruct | $0.44 | $1.10 | — | — | cloud.tencent.com/document/product/1729/97731 |
| Hunyuan Hy-2.0 Think | $0.55 | $2.21 | — | — | cloud.tencent.com/document/product/1729/97731 |
| Hunyuan A13B | $0.07 | $0.28 | — | — | cloud.tencent.com/document/product/1729/97731 |

Note: CNY-denominated prices (MiniMax, Kimi, Moonshot, Hunyuan) are converted at ~7.2 CNY/USD (June 2026 approximation).

## Custom Pricing Override

Users can supply custom per-model pricing via `.jenny/pricing.json` in the project directory:

```json
{
  "claude-sonnet-4-20250514": {
    "InputUSD": 0.000003,
    "OutputUSD": 0.000015,
    "CacheReadUSD": 0.0000003,
    "CacheCreationUSD": 0.00000375
  }
}
```

- File entries take precedence over `DefaultPricing`
- Malformed JSON or invalid field values produce a logged warning (not fatal) and fall through to default pricing
- Path resolved relative to project working directory

## Persistence

After each turn (or on shutdown), save to project config:

```json
{
  "lastSessionId": "sess_…",
  "lastModelUsage": { "claude-sonnet-4-…": { "inputTokens": 1000, … } },
  "totalCostUSD": 0.042
}
```

**Backward compatibility:** Old configs with `currency` and `total_cost_cny` fields are silently ignored (Go JSON `omitempty` + missing struct fields).

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
- Zero or negative budget means no limit.

## Edge Cases

| Case | Expected behavior |
|------|-------------------|
| Unknown model in pricing table | Set `hasUnknownModelCost`; use conservative default pricing; still track tokens |
| Retry after 429 | Count tokens from successful attempt only (or sum per policy) |
| Compaction turn | Compaction agent usage counted separately or excluded per config |
| Resume with wrong ID | Zero restored cost |
| Fast mode / advisor usage | Merge advisor token counts if enabled |

## Headless Protocol Compatibility

- Terminal `result` line always present on success (stream-json).
- Usage fields must match snake_case schema expected by SDK consumers.
- `session_id` on result matches init line.
- `total_cost_cny` field never emitted in stream JSON.

## Acceptance Criteria

- **AC1:** All four token types tracked per model when API returns them.
- **AC2:** Cost persists to project config with `lastSessionId`.
- **AC3:** Resume restores cost only on ID match.
- **AC4:** Stream-json `result.usage` includes cache token fields.
- **AC5:** `maxBudgetUsd` stops loop before exceeding budget.
- **AC6:** `DefaultPricing` entries reflect real provider USD rates with source citations.
- **AC7:** Config-based pricing override via `.jenny/pricing.json`.
- **AC8:** CNY-specific tests removed or converted.