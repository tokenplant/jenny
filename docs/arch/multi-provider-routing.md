---
title: Multi-Provider Routing
slug: multi-provider-routing
priority: P2
status: not_started
spec: complete
code: not_started
package: internal/api, internal/agent
depends_on:
  - anthropic-api-client
  - message-normalization
  - rate-limit-handling
---

# Multi-Provider Routing

Specifications for high-availability routing across multiple LLM providers, models, and API keys. This system ensures service continuity, cost optimization, and capability-based routing (e.g., vision) while preserving Prompt Caching through session stickiness.

## Design Goals

- **High Availability**: Automatic fallback when a provider is down or a key is rate-limited.
- **Cost Optimization**: Prefer low-cost models (e.g., DeepSeek) for standard tasks.
- **Capability Routing**: Seamlessly switch to specialized models (e.g., Claude 3.5 Sonnet) for vision or high-reasoning tasks.
- **Cache Preservation**: Maintain session stickiness to a single model/provider to maximize Prompt Caching efficiency.
- **Load Balancing**: Distribute load across different sessions using Round-Robin or Random selection.

## Configuration Schema

The configuration is split into **Resource Definitions** (Providers) and **Execution Policies** (Profiles).

### 1. Resource Definitions (Providers)

Defines available backends, models, and credentials.

```yaml
providers:
  - name: "deepseek"
    type: "openai"              # Protocol: openai, anthropic, gemini
    base_url: "https://api.deepseek.com"
    accounts:
      - name: "personal"
        keys: ["sk-ds-1", "sk-ds-2"] # Round-Robin across keys within an account
        priority: 1                  # Lower is higher priority
    models:
      - name: "deepseek-chat"
        tags: ["cheap", "text"]
        priority: 1
        context_window: 64000
        max_output: 4000
```

### 2. Execution Policies (Profiles)

Defines how resources are selected and used.

```yaml
profiles:
  default:
    # Ordered fallback chain
    targets:
      - match: { models: ["deepseek:deepseek-chat"] }
      - match: { tags: ["cheap"] }

    # Session persistence
    # sticky: Lock to one endpoint per session (protects Cache)
    # balanced: Re-evaluate per turn (sacrifices Cache for load distribution)
    routing_mode: "sticky"

    # Strategy for multiple matching candidates with equal priority
    # round_robin: Load balance across sessions
    # random: Random selection
    selection_policy: "round_robin"

    # Behavior for current endpoint failures
    retry_policy:
      on_rate_limit:
        max_retries: 3           # Exponential backoff to stay on current Cache
        backoff: "exponential"
      on_server_error:
        max_retries: 2

    # Whether to switch to the next target if retry fails
    allow_fallback: true
```

## Routing & Fallback Logic

The routing engine operates on a three-layered recovery logic to balance availability and cache efficiency.

### Layer 1: Sticky Retry (Preserve Cache)
- **Condition**: 429 (Rate Limit) or 5xx (Server Error).
- **Action**: Backoff and retry on the **same Key and Model**.
- **Goal**: Protect the existing Prompt Cache on the provider's side.

### Layer 2: Key Failover (Preserve Cache)
- **Condition**: Layer 1 retries exhausted, or 401 (Invalid Key).
- **Action**: Switch to the next available Key/Account for the **same Model**.
- **Goal**: Maintain stickiness to the model. Caches are often shared across different keys for the same model/provider.

### Layer 3: Model Fallback (Sacrifice Cache)
- **Condition**: All keys for the current model are exhausted, or `allow_fallback: true` with a permanent failure.
- **Action**: Move to the next `match` entry in the Profile's `targets` list.
- **Goal**: Ensure task completion at the cost of cache efficiency. Once a fallback occurs, the session locks (sticky) to the new endpoint.

## Load Balancing

- **Intra-Account**: `keys` within an `Account` are always used in a Round-Robin fashion to distribute quota usage.
- **Cross-Session**: When a new session starts, `selection_policy: round_robin` ensures that different sessions pick different candidates from the pool of matching models, preventing a single provider from being overwhelmed.

## Default Values

When a field is omitted from the configuration, the following defaults apply:

| Field | Default Value |
|-------|---------------|
| `priority` (Account/Model) | `1` |
| `routing_mode` | `sticky` |
| `selection_policy` | `round_robin` (within same priority) |
| `max_retries` | `3` |
| `backoff` | `exponential` |
| `allow_fallback` | `true` |

## Backward Compatibility

To ensure a seamless transition, the system automatically translates legacy environment variables into internal Provider and Profile definitions if no explicit YAML configuration is found:

1.  **Environment Mapping**:
    - `ANTHROPIC_API_KEY` / `ANTHROPIC_MODEL` -> Becomes a provider named `legacy-anthropic`.
    - `OPENAI_API_KEY` / `OPENAI_MODEL` / `OPENAI_BASE_URL` -> Becomes a provider named `legacy-openai`.
2.  **Implicit Profile**:
    - A `default` profile is synthesized that prioritizes the detected environment-based providers.
3.  **Coexistence**:
    - If a YAML configuration is present, it **takes precedence** over environment variables, but the Router may still allow merging environment-based "Ad-hoc" keys into a temporary `legacy` provider for debugging.

## Implementation Notes

- **Global Registry**: A singleton that tracks health status and rate-limit cooldowns for all configured Keys and Accounts.
- **StickyClient**: An `api.Requester` implementation that wraps the routing logic and holds the session-specific `ActiveEndpoint`.
- **Subagent Integration**: Subagents can specify a profile (e.g., `invoke_agent(profile="vision")`) to trigger specialized routing while inheriting the main session's context.
