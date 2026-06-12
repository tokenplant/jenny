# API Provider Architecture

The `internal/api/` package provides a unified interface for calling LLM APIs, with pluggable backends ("providers").

## Core Interface

```go
// Provider defines the interface for AI backend providers.
type Provider interface {
    SendMessage(ctx, messages, tools, toolResults, systemPrompt) (*Response, error)
    SendMessageStream(ctx, messages, tools, toolResults, systemPrompt, idleTimeout) (<-chan StreamContentBlock, *StreamResult)
    Kind() ProviderKind
}
```

## Provider Kinds

| Kind | Implementation |
|------|---------------|-------|
| `anthropic` | `provider_anthropic.go` |
| `openai` | `provider_openai.go` |
| `genai` | `provider_genai.go` |

## Provider Selection

`NewClientWithModel(model)` selects the provider at client creation time based on environment variables:

1. If `OPENAI_BASE_URL` is set → `openAIProvider`
2. If `GENAI_API_KEY` is set → `genaiProvider` (Gemini API backend)
3. If `GOOGLE_CLOUD_PROJECT` and `GOOGLE_CLOUD_LOCATION` are set → `genaiProvider` (Vertex AI backend with ADC)
4. Otherwise → `anthropicProvider` (default)

## Core Implementation Strategy: Surgical HTTP Clients

In June 2026, the architecture underwent a major optimization to address "SDK Bloat." The official SDKs (`openai-go`, `anthropic-sdk-go`, `google.golang.org/genai`) were found to contribute nearly 40MB of binary bloat due to extensive code generation for unsupported endpoints (e.g., Audio, Fine-tuning) and heavy use of generics.

Jenny now implements a **Surgical HTTP Client** approach:
1. **Lightweight Type Definitions:** We define only the necessary `struct` types for the endpoints we use (e.g., `/chat/completions`, `/v1/messages`). These are located in `*_types.go` files (e.g., `openai_types.go`).
2. **Polymorphic Field Handling:** We use `json.RawMessage` and custom marshaling logic to robustly handle complex, multi-modal API fields (like Anthropic's `content` arrays or Gemini's `parts`) without needing thousands of generated helper methods.
3. **Common Transport:** A shared `HTTPClient` (`http.go`) handles retries, context cancellation, and Server-Sent Events (SSE) parsing.
4. **Binary Size Impact:** This approach reduced the stripped binary size from ~34MB to **<8MB**, significantly improving startup time and distribution size.

## Adding a New Provider

1. **Create the provider file** in `internal/api/provider_<name>.go`
2. **Implement the `Provider` interface** with `SendMessage`, `SendMessageStream`, and `Kind()`
3. **Add to `NewClientWithModel`** in `client.go` — add an env var check in the selection chain
4. **Add provider kind constant** in `provider.go` if you need a distinct `ProviderKind`
5. **Add tests** — follow the pattern in `provider_openai_test.go`
6. **Update this doc**

### Interface Contract

- `SendMessage` must return a `*Response` with at minimum `Content`, `StopReason`, `Model`, and `Usage` fields populated
- `SendMessageStream` runs in a goroutine, yields `StreamContentBlock` via the channel, and returns a `*StreamResult` when the channel closes
- Both methods call `NormalizeMessages(messages, tools, Capabilities{...})` before building the request
- Both methods must set `StreamResult.StreamComplete = true` only when a terminal stop reason is received
- `StreamResult.Error` should be a plain string (not wrapped error type) for downstream compatibility
- Providers should implement `ProviderWithRetryConfig` to receive shared retry configuration

### Normalization

`NormalizeMessages` is a shared gateway that all providers call. It handles:
- Injecting `__arg__` placeholder for empty tool schemas
- Deduplicating tool results by `ToolUseID`

Providers set `SupportsPromptCaching: true` or `false` in `Capabilities` to control prompt-caching-specific normalization.

### Retry Logic

The `RetryConfig` struct in `retry.go` provides shared retry configuration. Providers should implement `ProviderWithRetryConfig` and call `sendWithRetry` with exponential backoff for HTTP errors. Retryable status codes: 429, 408, 409, 500, 502, 503, 504, 529.

## Shared Types

All types are defined in `client.go`:
- `Message`, `ToolUseBlock`, `ToolResultBlock`, `ToolResult`, `ToolUse`
- `Response`, `ContentBlock`, `Usage`, `ToolParam`, `ToolInputSchema`
- `StreamContentBlock`, `StreamResult`

## Environment Variables

### OpenAI Provider
- `OPENAI_BASE_URL` — API base URL (required)
- `OPENAI_API_KEY` — API key (required)
- `OPENAI_DEFAULT_MODEL` — default model (required when using OpenAI provider)
- `OPENAI_WIRE_API` — wire protocol (`chat` only; `responses` not yet supported)

### Anthropic Provider
- `ANTHROPIC_BASE_URL` — API base URL
- `ANTHROPIC_AUTH_TOKEN` — API key
- `ANTHROPIC_MODEL` — default model
- `ANTHROPIC_BETAS` — comma-separated list of additional beta headers
- `API_TIMEOUT_MS` — request timeout in milliseconds

### GenAI Provider (Gemini / Vertex AI)
The `genaiProvider` is backed by Google's official `google.golang.org/genai` Go SDK. It can target either the public Gemini API (`BackendGeminiAPI`) or Vertex AI (`BackendVertexAI`) — selection is automatic from environment variables.

Environment variables (in precedence order, higher wins):
- `GENAI_BASE_URL` — override the API base URL (e.g. proxy or VPC endpoint). Optional.
- `GENAI_API_KEY` — explicit API key. Highest precedence; bypasses the SDK's built-in `GOOGLE_API_KEY` / `GEMINI_API_KEY` lookups.
- `GOOGLE_API_KEY` / `GEMINI_API_KEY` — Gemini API key (read by the SDK when no explicit key is set).
- `GOOGLE_CLOUD_PROJECT` — required to select the Vertex AI backend.
- `GOOGLE_CLOUD_LOCATION` (or `GOOGLE_CLOUD_REGION`) — required to select the Vertex AI backend.
- `GOOGLE_GENAI_USE_VERTEXAI=1|true` — force the Vertex AI backend.
- `GENAI_DEFAULT_MODEL` — default model (required when using the genai provider).

Selection rules (mirrors the SDK's own logic):
1. If `GENAI_API_KEY` is set explicitly → Gemini API backend.
2. Else if `GOOGLE_CLOUD_PROJECT` and `GOOGLE_CLOUD_LOCATION` are set → Vertex AI backend with Application Default Credentials.
3. Else if `GOOGLE_API_KEY` / `GEMINI_API_KEY` is set → Gemini API backend.
4. Otherwise the provider constructor returns an error.

Behavior:
- Non-streaming and streaming requests use `client.Models.GenerateContent` and `client.Models.GenerateContentStream` respectively.
- System prompts are concatenated (`systemPrompt` + `"\n\n" + systemPromptSuffix`) and passed via `GenerateContentConfig.SystemInstruction`. (The SDK does not expose a per-segment cache-control knob, so the cached-stable/per-turn split is collapsed.)
- Tools are translated from `ToolParam` to `*genai.Tool` with a `*genai.FunctionDeclaration` per tool. Empty property sets receive a synthetic `__arg__: string` placeholder.
- Tool results are fed back as `*genai.FunctionResponse` parts on a `RoleUser` content turn, paired with the model's prior `*genai.FunctionCall` turn (the SDK requires both to be present in the conversation history).
- Errors are mapped to `*RetryableHTTPError` (or returned unwrapped) based on `genai.APIError.Code` so the existing retry/streaming-fallback machinery works without changes.
- Usage tokens: `PromptTokenCount → InputTokens`, `ResponseTokenCount → OutputTokens`, `CachedContentTokenCount → CacheReadInputTokens`, `ThoughtsTokenCount` is folded into `OutputTokens` (matching the Anthropic behavior where thinking tokens are part of the output budget).