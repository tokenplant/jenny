---
status: done
code: done
---
# Provider-Aware Fixes → Universal Normalization

## Context

This document previously described provider-specific shims gated on URL-based provider detection. The architecture has since been refactored to use **universal normalization** — all fixes now apply unconditionally to every provider, eliminating provider-specific code paths.

See [`universal-normalization-architecture.md`](./universal-normalization-architecture.md) for the current architecture.

## Normalization Pass Map

The following passes were previously provider-aware but are now universal:

| Pass | Trigger | Commit | Notes |
|------|---------|--------|-------|
| Tool Result Dedup | Every `tool_result` block | a154210 | Deduplicated by `tool_use_id` (last-writer-wins). Now universal via `NormalizeMessages`. |
| Empty Schema Placeholder | Tools with empty `input_schema.properties` | 127a1b5 | Injects `__arg__: {type: string}` placeholder. Now universal via `NormalizeMessages`. |

## Why Universal?

The previous URL-based detection (`providerFromBaseURL()`) was fragile — it relied on substring matching in the base URL and required separate code paths for each provider. The universal approach:

1. Applies fixes unconditionally to all outgoing payloads
2. Eliminates provider-specific branching in the serialization path
3. Guarantees compatibility across all API providers without detection

## Migration Notes

- `providerFromBaseURL()` has been removed
- Tool serialization no longer branches on provider type
- `NormalizeMessages` in `internal/api/` is the single gateway before JSON serialization
- All existing tests pass without modification

## References

- [`universal-normalization-architecture.md`](./universal-normalization-architecture.md) — Current architecture source of truth
- [`anthropic-api-client.md`](./anthropic-api-client.md) — Provider Compatibility section updated
