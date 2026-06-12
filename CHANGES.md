# OpenAI Responses API Feature (v1.x)

## Changes

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
- BLK2 fixed: thinkingBlocks now persisted in engine_loop.go
- BLK3 fixed: RebuildMessages preserves Thinking/Signature from transcript
- Anthropic thinking blocks emitted BEFORE tool_use in content array (AC4)

### DeepSeek Integration
- Thinking mode support via `extra_body: {"thinking": {"type": "enabled"}}`
- `reasoning_content` parsed and stored

### Test Coverage
- Added thinking persistence round-trip tests for OpenAI Chat, Anthropic, and Responses API
- Multi-turn conversation tests with thinking across multiple turns
- Backward compatibility tests for older transcripts without thinking fields