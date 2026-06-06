---
title: CLI
slug: cli
priority: P0
status: partial
spec: complete
code: partial
package: internal/cli
gaps:
  - --mcp-config not wired
  - --continue, --fork-session, --no-session-persistence missing
depends_on:
  []
---
# CLI

## Overview

Jenny CLI is headless-only: accept a prompt, run the agent loop, emit text or stream-json, exit with status code.

## Usage

```bash
jenny [flags] [prompt]
jenny -p "prompt text"
```

## Flags

| Flag | Description |
|------|-------------|
| `-p`, `--print <prompt>` | Prompt string (non-interactive) |
| `--model <name>` | Override model (beats `ANTHROPIC_MODEL` env) |
| `-r`, `--resume <session_id>` | Resume session from transcript |
| `--continue` | Resume most recent session in project |
| `--fork-session` | Fork resumed session to new ID |
| `--resume-session-at <uuid>` | Truncate chain at message (requires `-r`) |
| `--output-format <fmt>` | `text` (default), `json`, `stream-json` |
| `--include-partial-messages` | Emit SSE partial events (requires stream-json + SSE) |
| `--mcp-config <path>…` | MCP config file(s) or inline JSON |
| `--strict-mcp-config` | Only use `--mcp-config` servers |
| `--no-session-persistence` | Disable transcript read/write |
| `--verbose` | Debug logging to stderr |
| `--dangerously-skip-permissions` | Bypass permission/classifier gates |

## Flag Rules

| Rule | Behavior |
|------|----------|
| No prompt | Print usage; exit non-zero |
| Positional + `-p` both given | Positional wins when `-p` empty; otherwise document explicit precedence |
| `--output-format stream-json` | Requires prompt (`-p` or positional) |
| `--include-partial-messages` | Requires `--output-format stream-json` |
| `--resume-session-at` | Requires `-r` / `--resume` |

## Exit Codes

| Code | Condition |
|------|-----------|
| 0 | Success |
| Non-zero | Missing prompt, API error, agent error, session not found |

Help (`-h`) exits 0.

## Environment Variables

| Variable | Description |
|----------|-------------|
| `ANTHROPIC_BASE_URL` | API endpoint |
| `ANTHROPIC_AUTH_TOKEN` | Auth token |
| `ANTHROPIC_MODEL` | Default model |
| `JENNY_DEBUG` | Enable debug slog (`1` = DEBUG) |

## Jenny Gaps vs Target Spec

| Feature | Status |
|---------|--------|
| `-r` resume | Wired |
| `--mcp-config` | Parsed, not wired |
| `--continue` | Not implemented |
| `--no-session-persistence` | Not implemented |
| `--fork-session` | Not implemented |
| stream-json stdout guard | Not implemented |
| Flat tool_use `parameters` | Should use `parameters` not `tool_input` |

## Acceptance Criteria

- **AC1:** No prompt → usage + non-zero exit.
- **AC2:** `--model` overrides env model.
- **AC3:** `-r` loads transcript and preserves session ID in output.
- **AC4:** stream-json writes JSON lines to stdout only.
- **AC5:** `--verbose` / `JENNY_DEBUG` logs to stderr without polluting stdout.

## Related

- Stream protocol: [`stream-json.md`](./stream-json.md)
- Session resume: [`session-resume.md`](./session-resume.md)
