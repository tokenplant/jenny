---
title: Rate Limit Handling
slug: rate-limit-handling
priority: P1
status: done
spec: complete
code: done
package: internal/api
gaps:
  []
depends_on:
  - anthropic-api-client
---
# Rate Limit Handling

## Overview

API client retries transient failures with exponential backoff. Foreground agent calls retry 529 overload errors; background classifiers do not.

## Retryable Conditions

| Condition | Retry |
|-----------|-------|
| HTTP 429 | Yes |
| HTTP 529 / overloaded_error | Yes (foreground, capped) |
| HTTP 408, 409 | Yes |
| HTTP 5xx | Yes |
| Connection errors | Yes |
| Mock rate limits | No |
| Non-retryable 4xx | No |
| Subscriber 429 with x-should-retry: false | No |

## Backoff

- Base delay: 500ms
- Exponential cap: 32s
- Jitter: 25%
- Honor `Retry-After` header when present
- Default max retries: 10 (env override)

## 529 Overload Cap

`MAX_529_RETRIES = 3` consecutive 529 errors:

- Then try fallback model if configured.
- Else throw `CannotRetryError` with message **Repeated 529 Overloaded errors**.

## Foreground vs Background

Background query sources (classifiers, summaries, memory extraction):

- **No** 529 retry (immediate `CannotRetryError`).
- Prevents retry amplification on auxiliary calls.

Foreground sources (main agent loop): full 529 retry budget.

## Retry Context Preservation

Each retry passes unchanged:

- `model`
- `thinkingConfig`
- `maxTokensOverride` (adjusted on max-output overflow)
- `fastMode`

## Max Output Token Recovery

On max-output-tokens stop reason:

- Retry with increased `maxTokensOverride` (bounded).
- Cap recovery retries to avoid infinite loops (see agent loop).

## Edge Cases

| Case | Expected behavior |
|------|-------------------|
| Model switch on fallback | Preserve messages; new model param |
| 529 during streaming | Count toward 529 budget; may trigger fallback |
| Budget exhausted mid-retry | Stop with distinct error |

## Acceptance Criteria

- **AC1:** 429 retried with backoff up to max retries.
- **AC2:** Fourth consecutive 529 fails with distinct error message.
- **AC3:** Background classifiers do not retry 529.
- **AC4:** Retry-After honored when set.
- **AC5:** Model and max_tokens preserved across retries.
