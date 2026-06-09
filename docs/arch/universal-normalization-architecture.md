# Universal Normalization Architecture

## Context
A headless coding agent communicating with large language models must manage complex state: long contexts, interrupted streams, tool errors, and resumed sessions. When communicating with the standard API or third-party proxies (e.g., LiteLLM, Bedrock, Vertex, or other compatible endpoints), even minor deviations in the expected payload structure can result in catastrophic `400 Bad Request` errors.

Historically, implementations have relied on "provider-aware fixes"—checking the endpoint URL and applying specific patches (e.g., "if MiniMax, do X", "if DeepSeek, do Y"). This approach is fragile and scales poorly. 

This document defines a **Universal Normalization Architecture**. Instead of guessing the provider, the agent must enforce a **Strict-Standard-Normal-Form (SSNF)** on all outgoing payloads. By preemptively repairing state to match the strictest possible interpretation of the API schema, the agent guarantees compatibility across all backends.

## The Normalization Pipeline

The normalization pipeline is a mandatory pre-flight phase that runs before every API request. It consists of multiple discrete passes over the message history and tool definitions.

### 1. Tool Schema Stabilization

Third-party providers and proxies often enforce stricter JSON schema validation than the primary API, rejecting experimental fields or empty schemas.

*   **No Empty Schemas:** Every tool must have a non-empty `properties` object within its `input_schema`. If a tool genuinely requires no arguments, the normalization layer must inject a safe, ignored placeholder property (e.g., `__arg__: string`).
*   **Beta Field Stripping (Feature Gating):** Experimental features (e.g., `defer_loading`, `cache_control`, `eager_input_streaming`) must be strictly gated. A `DISABLE_EXPERIMENTAL_BETAS` flag or capability map must dictate whether these fields are included. By default, when communicating with unknown endpoints, the schema must be stripped down to the bare minimum: `name`, `description`, and `input_schema`.

### 2. Message History Repair (The SSNF Pass)

The bulk of API errors stem from malformed message histories, particularly after interruptions, tool execution failures, or when resuming an older session transcript. The following rules must be applied sequentially to the message array.

#### A. Role Alternation & Merging
The API mandates strictly alternating `user` and `assistant` roles.
*   **Merge Adjacent User Messages:** If multiple `user` messages appear sequentially, their `content` arrays must be merged into a single `user` message.
*   **Merge Adjacent Assistant Messages:** If an agent outputs multiple `assistant` messages in a row (e.g., during complex tool usage or streaming chunk boundaries), they must be merged if they share the same underlying message ID.

#### B. Tool Pairing and Deduplication
The API requires perfect symmetry between tool requests and results.
*   **Global Tool ID Deduplication:** Walk the entire history to ensure every `tool_use_id` is unique. If an assistant message contains a duplicate `tool_use` (which can occur during malformed stream resumes), the duplicate must be silently stripped.
*   **Strict Pairing:** Every `tool_use` must have a corresponding `tool_result` in the immediately following `user` message.
    *   **Missing Results:** If a `tool_use` exists without a result (e.g., the user interrupted the process before the tool finished), the normalization layer must synthesize an error result: `{"type": "tool_result", "tool_use_id": "<id>", "is_error": true, "content": "[Tool use interrupted]"}`.
    *   **Orphaned Results:** If a `user` message contains a `tool_result` pointing to a non-existent `tool_use_id`, that `tool_result` must be stripped to prevent API rejection.

#### C. Content Block Validation
Specific block types have strict content requirements that models occasionally violate, or that become invalid due to session boundaries.
*   **Whitespace-Only Assistant Messages:** The API rejects `assistant` text blocks containing only whitespace (e.g., `\n\n`). If an assistant message consists entirely of whitespace blocks, it must be removed from the history (which may necessitate re-running the Role Alternation pass).
*   **Empty Assistant Content:** While the *final* assistant message can be empty (for prefill), historical assistant messages cannot. If an intermediate assistant message has an empty content array, a placeholder text block (e.g., `[No content]`) must be inserted.
*   **Trailing Thinking Blocks:** Assistant messages cannot end with `thinking` or `redacted_thinking` blocks. If they do, the trailing blocks must be stripped until a valid block (text or tool_use) is found, or replaced with a placeholder if the message becomes empty.
*   **Orphaned Thinking Blocks:** If a resumed session leaves an assistant message containing *only* thinking blocks (with no corresponding text or tool_use sharing the same message ID), the entire message must be dropped.

#### D. Credential-Bound Artifact Stripping
Certain API features generate artifacts bound to a specific API key or session (e.g., `redacted_thinking` blocks or specific connector tags).
*   If a session is resumed with a different API key, or if the endpoint proxy rejects these artifacts, all signature-bearing blocks (like `redacted_thinking`) must be systematically stripped from historical `assistant` messages before transmission.

### 3. Error Handling and Retry Capabilities

The normalization architecture also governs how the agent reacts to API errors. Instead of crashing, the system must interpret standard HTTP errors and adjust its payload dynamically.

*   **400 Tool Use Mismatch:** If an endpoint still returns a concurrency or mismatch error, the system must log the failure and prompt the user to rewind the conversation state.
*   **413 / Media Size Errors:** If the API rejects a payload due to size limits (e.g., "image exceeds maximum dimensions" or "PDF too large"), the normalization layer must tag the specific `user` message and strip the offending document/image block on all subsequent retries, replacing it with a text placeholder to prevent endless retry loops.

## Implementation Guidelines

To implement this architecture safely:

1.  **Centralized Pipeline:** Create a single, exhaustive `NormalizeMessages(messages []Message, tools []Tool, capabilities Capabilities)` function. This must be the final gateway before JSON serialization.
2.  **Immutability:** The normalization process must operate on a clone of the message history. The on-disk transcript should reflect the raw, true history; the normalized payload is strictly ephemeral for the API request.
3.  **Metrics and Logging:** Every time the normalization layer mutates the history (e.g., repairing a missing tool result or stripping an empty message), it must log the intervention. High rates of normalization repairs indicate systemic issues in the state management layer.

By adhering to this Strict-Standard-Normal-Form, the agent ensures maximum resilience and guarantees seamless operation across any API endpoint claiming Anthropic compatibility.