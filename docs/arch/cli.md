---
title: CLI
slug: cli
priority: P0
status: done
spec: complete
code: done
package: internal/cli
gaps: []
defer_to: P3
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
| `--version`, `-v` | Prints `<semver> (jenny)` and exits 0. |
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
| `--system-prompt <text>` | Replace the default system prompt entirely |
| `--append-system-prompt <text>` | Append text after the assembled default system prompt |
| `--print-system-prompt` | Print the assembled system prompt and exit (no API call) |

## Flag Rules

| Rule | Behavior |
|------|----------|
| No prompt | Print usage; exit non-zero |
| Positional + `-p` both given | Positional wins when `-p` empty; otherwise document explicit precedence |
| `--output-format stream-json` | Requires prompt (`-p` or positional) |
| `--include-partial-messages` | Requires `--output-format stream-json` |
| `--resume-session-at` | Requires `-r` / `--resume` |
| `--continue` with no prior sessions | Exit non-zero with error "no sessions to continue" |

## Exit Codes

| Code | Condition |
|------|-----------|
| 0 | Success |
| Non-zero | Missing prompt, API error, agent error, session not found |
| Non-zero | Unknown or invalid flag |

Help (`-h`) exits 0.

## Environment Variables

| Variable | Description |
|----------|-------------|
| `ANTHROPIC_BASE_URL` | API endpoint |
| `ANTHROPIC_AUTH_TOKEN` | Auth token — forwarded as `Authorization: Bearer <token>` |
| `ANTHROPIC_API_KEY` | API key sent as `X-Api-Key` header. When set, takes precedence over `ANTHROPIC_AUTH_TOKEN`. |
| `ANTHROPIC_BETAS` | Comma-separated list of additional `anthropic-beta` header values. |
| `ANTHROPIC_MODEL` | Default model — overridden by `--model` flag when both are set |
| `API_TIMEOUT_MS` | Timeout for API requests in milliseconds (default: 3600000, or 60 minutes). |
| `DEBUG` | Enable debug logging. Values: `1`, `true`, `yes`, `on`. Alias for `JENNY_DEBUG`. |
| `HTTP_PROXY` | HTTP proxy URL for API requests. |
| `HTTPS_PROXY` | HTTPS proxy URL for API requests. |
| `JENNY_DEBUG` | Enable debug slog (`1` = DEBUG) |
| `JENNY_TRANSCRIPT_DIR` | Override transcript directory (default: `~/.jenny/transcripts`) |
| `NO_PROXY` | Comma-separated list of domains to bypass proxy for. |

## Jenny Gaps vs Target Spec

| Feature | Status |
|---------|--------|
| `-r` resume | Wired |
| `--mcp-config` | Wired |
| `--continue` | Wired |
| `--no-session-persistence` | Wired |
| `--fork-session` | Wired |
| stream-json stdout guard | Wired |

## Acceptance Criteria

- **AC1:** No prompt → usage + non-zero exit.
- **AC2:** `--model` overrides env model.
- **AC3:** `-r` loads transcript and preserves session ID in output.
- **AC4:** stream-json writes JSON lines to stdout only.
- **AC5:** `--verbose` / `JENNY_DEBUG` logs to stderr without polluting stdout.

## Related

- Stream protocol: [`stream-json.md`](./stream-json.md)
- Session resume: [`session-resume.md`](./session-resume.md)
