# Changelog

All notable changes to jenny will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.17.0] - 2026-06-15

### Completed
- **stream-json alignment**: Project goal fully achieved after 17 iterations
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
