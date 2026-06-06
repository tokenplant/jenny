---
title: Dangerous Command Gate
slug: dangerous-command-gate
priority: P1
status: not_started
spec: complete
code: not_started
package: internal/tool
gaps:
  - Bash has prefix allowlist only; no substitution/pipeline analysis
depends_on:
  - tool-registry
---
# Dangerous Command Gate

## Overview

Before Bash execution, commands pass security validation independent of sandbox. Read-only mode adds pipeline-level checks. `--dangerously-skip-permissions` bypasses classifier only when explicitly set.

## Blocked Patterns (All Modes)

| Category | Examples |
|----------|----------|
| Command substitution | `$()`, `${}`, backticks |
| Process substitution | `<()`, `>()`, `=()` |
| Zsh extras | `=cmd`, `$[`, `~[` |
| ANSI-C / locale quoting tricks | Exploit tokenization differential |
| Brace expansion mismatch | Unbalanced `{}` |
| Carriage return smuggling | `\r` in tokens |
| Device paths | `/dev/zero`, `/dev/urandom`, `/dev/random`, `/dev/full`, stdio fds |
| Proc environ | `/proc/*/environ` |
| Git config injection | `git … -c`, `--exec-path`, `--config-env` |

## Read-Only Pipeline Validation

Every pipeline segment must pass read-only allowlist:

- Semantic-neutral commands (`echo`, `true`) skipped in `||` chains only.
- Unquoted `$VAR` / globs fail read-only check.
- `cd && git` escape patterns blocked.
- All segments must be read/search commands.

Prefer flag-level validation over regex where possible.

## Classifier Layer

Auto permission mode uses prompt classifier (`bashClassifier`, `yoloClassifier`) for ambiguous commands.

Background classifier calls: no 529 retry (see rate-limit doc).

## Bypass

`--dangerously-skip-permissions` → permission mode `bypassPermissions`:

- Skips classifier and security checks in main permission flow.
- Must be explicit CLI flag; never default in headless production.

## Read Tool Device Blocks

Same device path blocklist for Read tool without reading content.

## Edge Cases

| Case | Expected behavior |
|------|-------------------|
| Nested substitution | Block even if inner looks safe |
| Unicode whitespace tricks | Normalize or reject |
| Heredoc injection | Block dangerous heredoc patterns |
| Read-only `git log` | Allow if no injection flags |

## Acceptance Criteria

- **AC1:** Command substitution blocked before execution.
- **AC2:** Read-only pipeline rejects mutating segment.
- **AC3:** Git `-c` injection blocked in read-only mode.
- **AC4:** Bypass only with explicit flag.
- **AC5:** Device paths blocked in Read and Bash.
