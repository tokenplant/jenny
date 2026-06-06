---
title: Sandbox Abstraction
slug: sandbox
priority: P2
status: not_started
spec: complete
code: not_started
package: internal/sandbox
gaps:
  []
depends_on:
  - bash
---
# Sandbox Abstraction

## Overview

Pluggable OS-level sandbox wraps Bash and optionally Grep ripgrep. Policy from settings: filesystem, network, managed-domains-only.

## Pluggable Backend

Interface `SandboxManager` wrapping external sandbox-runtime.

Platforms: macOS, Linux, WSL2 (not WSL1).

- `initialize()` builds config from settings.
- `wrapWithSandbox(command)` returns wrapped shell command.
- `refreshConfig()` after permission changes.
- `failIfUnavailable`: clear error when sandbox enabled but deps missing.

## Per-Invocation Opt-Out

- `sandbox.excludedCommands[]`: patterns not wrapped.
- `sandbox.allowUnsandboxedCommands`: policy for non-sandboxed bash when sandbox on.
- Bash `dangerouslyDisableSandbox` per call.

## Network Policy

| Mode | Behavior |
|------|----------|
| Normal | Merge allowedDomains + WebFetch allow rules |
| managed-domains-only | **Only** policy domains; block interactive ask |

Denied domains from permission deny rules always applied.

## Sandboxed Ripgrep

Config `sandbox.ripgrep`: `{ command, args, argv0 }`.

Grep tool uses sandboxed ripgrep when sandbox active.

## Filesystem Policy

- Allow write: `.`, temp dir, `--add-dir` paths, worktree main `.git` when detected.
- Deny write: settings files, skills dir, bare-repo escape files when present.

## Edge Cases

| Case | Expected behavior |
|------|-------------------|
| Worktree main repo | Cache at init for index.lock writes |
| Linux glob in permissions | Warn via getLinuxGlobPatternWarnings |
| Policy-locked settings | Override local changes |
| Sandbox off | Grep uses host ripgrep |

## Acceptance Criteria

- **AC1:** Bash wrapped unless excluded pattern matches.
- **AC2:** Managed-domains-only restricts network to policy list.
- **AC3:** Grep uses sandboxed ripgrep when sandbox on.
- **AC4:** Missing deps yield clear unavailable reason.
- **AC5:** refreshConfig without restart.
