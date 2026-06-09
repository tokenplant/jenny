---
status: done
code: n/a
---
# Provider-Aware Fixes

## Context

Jenny is an independent Go reimplementation of a headless coding agent. It is not a fork of the Claude Code binary, nor a wrapper around it. Jenny speaks the Anthropic Messages API directly — the same wire protocol used by Claude Code and official SDKs.

Each API provider that implements the Anthropic Messages API has actual conformance gaps relative to the published spec. These gaps cause real production errors: `error 2013` from MiniMax, 400 rejections from DeepSeek, and so on.

Claude Code may paper over these gaps via its own private provider-routing code, which is inside the Claude Code binary and not accessible to external tools. Jenny does not have access to that routing layer. When a provider deviates from the spec, jenny must carry its own explicit shim to remain compatible.

## Decision

Carry provider-specific shims in jenny's API client layer, gated on explicit provider detection via `ANTHROPIC_BASE_URL`. Each shim is isolated, documented, and tied to the commit that introduced it.

This is not a design flaw — it is the correct trade-off for a multi-provider headless agent. Removing these shims would mean dropping provider support or shipping known breakage.

## Provider-Fix Map

| Provider | Symptom | Fix | Trigger commit |
|----------|---------|-----|----------------|
| DeepSeek | server rejects request when two `tool_result` blocks share a `tool_use_id` | deduplicate `tool_result`s by `tool_use_id` (last-writer-wins) in `mergeConsecutiveSameRole` and in `api.SendMessage` serialization | a154210 |
| MiniMax | error 2013 when a tool has empty `input_schema.properties` | inject a `__arg__: {type: string}` placeholder property when serializing | 127a1b5 |

Both shims are provider-aware: they apply only when `ANTHROPIC_BASE_URL` contains a known provider substring. For the standard Anthropic endpoint, tool serialization is unchanged.

See [`anthropic-api-client.md`](./anthropic-api-client.md) (Provider Compatibility section) for the full technical detail on each fix.

## Alternatives Considered

### (a) Rely on Anthropic-format-only and drop non-Anthropic providers

**Rejected.** The project's goal is multi-provider support. Dropping providers to simplify the wire format would contradict that goal. Users who run against DeepSeek or MiniMax need working integrations, not error messages telling them to switch back to Anthropic.

### (b) Shell out to the Claude Code binary as a library

**Rejected.** Claude Code is a separate product with a different scope — it is an interactive TUI with IDE integrations. Depending on it as a library would invert jenny's "headless Go agent" premise and introduce an unwanted runtime dependency on a UI-oriented binary. Additionally, Claude Code does not expose a stable library API for headless use.

### (c) Upstream the fixes into the providers themselves

**Deferred.** Filing provider conformance bugs is the right long-term move, but it is not actionable inside jenny's release cadence. Provider teams may or may not act on the feedback in time for users who need working integrations today. Carrying the shim is the pragmatic path until providers close the gap.

## Consequences

- **(i)** jenny's API client must carry provider detection logic — currently URL-based via `ANTHROPIC_BASE_URL`, gated in `providerFromBaseURL()`.
- **(ii)** every new provider integration may surface new shims — this is expected and should be documented in this file when it happens.
- **(iii)** shims are an ongoing maintenance cost but are strictly cheaper than dropping provider support — the alternative is worse.