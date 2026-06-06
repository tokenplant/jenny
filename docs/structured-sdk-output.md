---
title: Structured SDK Output
slug: structured-sdk-output
priority: P4
status: not_started
spec: complete
code: not_started
package: internal/agent
gaps:
  []
depends_on:
  - query-engine
---
# Structured SDK Output

## Overview

Enforces JSON schema output via synthetic StructuredOutput tool in non-interactive sessions.

## Requirements

- JSON schema param on QueryEngine/SDK entry
- Synthetic `StructuredOutput` tool in tool pool
- Model must support structured outputs
- Per-session enforcement hook

## Validation

- Ajv validates schema at tool creation
- Validates input at tool invocation
- Invalid schema → error at tool registration
- Model must emit exactly one StructuredOutput call at end of turn

## Acceptance Criteria

- **AC1:** Schema + output tool both required.
- **AC2:** Invalid schema fails at registration.
- **AC3:** Exactly one structured output call enforced.
- **AC4:** Non-interactive sessions only.
