# OpenAI Responses API Feature (v1.x)

## Changes

## WebUI Portal (v0.4.0)
- **Status: Production Ready**
- Full implementation of all 7 tabs (Dashboard, Sessions, Skills, MCP, Plugins, Cost, Settings)
- Embedded frontend in Go binary for single-executable distribution
- Cross-platform support for Darwin, Linux, and Windows
- SSE streaming for real-time session updates
- Secure token-based authentication
- Multi-language support (en, zh-Hans, zh-Hant)

### Responses API Support
- New provider: `openAIResponsesProvider` for `/v1/responses` endpoint
- Types: `OpenAIResponsesRequest/OpenAIResponsesResponse` with request/response structs
- Client selection via `OPENAI_WIRE_API` env var (`chat` or `responses`)

### Reasoning Effort Control
- CLI flag `--effort low|medium|high` threaded to providers
- `reasoning_config.effort` for Responses API
- `reasoning_effort` for Chat API
- `SetThinkingConfig` method on providers

### Thinking Block Persistence
- Transcript entries now include `thinking` and `signature` fields
- Round-trip support for thinking blocks in multi-turn conversations
- BLK-PERS fixed: thinking content now correctly persisted in assistant messages
- BLK-REBLD fixed: RebuildMessages preserves Thinking/Signature from transcript
- Anthropic thinking blocks emitted BEFORE tool_use in content array (AC4)

### DeepSeek Integration
- Thinking mode support via `extra_body: {"thinking": {"type": "enabled"}}`
- `reasoning_content` parsed and stored
- Automatic detection of DeepSeek models to inject provider-specific headers