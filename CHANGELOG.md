# Changelog

All notable changes to jenny will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.17.0] - 2026-06-15

### Completed
- **stream-json alignment**: Project goal fully achieved after 20 iterations
  - All gap tests pass: TestStreamJSONGap_ResultExtendedFields and TestStreamJSONGap_InitExtendedFields
  - All stream-json format differences resolved with Claude Code headless behavior
  - Init event fields: fast_mode_state, output_style, mcp_servers, apiKeySource, analytics_disabled, skills
  - Parent_tool_use_id omission when nil
  - Result event telemetry: ttft_ms, ttft_stream_ms, time_to_request_ms, terminal_reason, api_error_status
  - Reference binary runner integrated in e2e suite
  - All documentation updated

### Changes
- Added analytics_disabled, apiKeySource, and skills to init event
- Always emit timing fields in result events
- Fixed ttft_stream_ms semantics and zero-value emission
- Fixed ttft_stream_ms and pendingDelay timing issues

## [0.4.0] - 2026-06-14

### WebUI Portal
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
