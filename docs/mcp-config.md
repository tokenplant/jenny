---
title: MCP Configuration
slug: mcp-config
priority: P0
status: partial
spec: complete
code: partial
package: internal/mcp
gaps:
  - "Managed-domains-only mode and enterprise policy enforcement not implemented"
  - "Orphaned plugin cache exclusion from search tools not implemented"
  - "Strict mode does not yet exclude all non-CLI sources (partially wired)"
  - "OAuth credentials and OpenID Connect Discovery not supported in server definition"
depends_on:
  - cli
---
# MCP Configuration

## Overview

Jenny loads Model Context Protocol (MCP) server definitions from config files and CLI flags. Headless operators pass `--mcp-config` to attach servers without interactive setup.

## CLI

| Flag | Behavior |
|------|----------|
| `--mcp-config <path>…` | One or more JSON file paths or inline JSON strings; repeatable |
| `--strict-mcp-config` | Use **only** `--mcp-config` servers; ignore user/project/local/plugin sources |

**Gap (Jenny today):** flag is wired to runtime (stdio transport only).

## Config Merge Precedence

When not in strict mode, configs merge from lowest to highest priority (later wins):

1. Plugin bundled configs
2. User config directory
3. Project config directory
4. Local / enterprise overrides

Enterprise connectors may inject additional policy constraints.

## Server Definition Shape

Each server entry supports:

```json
{
  "mcpServers": {
    "my-server": {
      "command": "npx",
      "args": ["-y", "@example/mcp-server"],
      "env": { "API_KEY": "${MY_API_KEY}" },
      "url": "https://example.com/mcp",
      "headers": { "Authorization": "Bearer ${TOKEN:-default}" }
    }
  }
}
```

### Environment Variable Expansion

Expand `${VAR}` and `${VAR:-default}` in:

- `command`, `args[]`, `env` values
- `url`, `headers` values

Unset `${VAR}` without default → empty string or validation error at load time.

## Plugin Orphaned Cache Exclusion

When plugins install MCP servers from zip caches, orphaned cache directories (plugin removed but cache remains) must be excluded from:

- Workspace search tools (Glob/Grep path roots)
- MCP server discovery paths

Prevents stale tools and path leakage from uninstalled plugins.

## Managed-Domains-Only Mode

Enterprise policy may set `allowManagedDomainsOnly` for MCP network egress:

- Only policy-allowed MCP servers connect.
- Denied servers blocked at config validation (`isMcpServerAllowedByPolicy`).
- Restricted plugin-only mode when enterprise locks MCP surface.

## Edge Cases

| Case | Expected behavior |
|------|-------------------|
| Invalid JSON in `--mcp-config` | Fail startup with parse error |
| Duplicate server names | Higher-precedence config wins |
| Missing env var for required secret | Fail at connect time with actionable message |
| Strict mode + empty `--mcp-config` | No MCP servers (not fallback to user config) |
| Relative paths in args | Resolved against cwd at launch |

## Headless Protocol Compatibility

- `system`/`init` stream-json line lists connected `mcp_servers` with name and status.
- Tool names exposed to model use prefix `mcp__<server>__<tool>` (see [`mcp-client.md`](./mcp-client.md)).

## Acceptance Criteria

- **AC1:** Multiple `--mcp-config` paths merge in order.
- **AC2:** Env expansion works for `${VAR}` and `${VAR:-default}`.
- **AC3:** Strict mode ignores non-CLI config sources.
- **AC4:** Orphaned plugin cache dirs excluded from search tools.
- **AC5:** Enterprise deny list blocks server before connection attempt.
