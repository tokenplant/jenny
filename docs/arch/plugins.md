---
title: Plugin System
slug: plugins
priority: P3
status: not_started
spec: complete
code: partial
package: internal/plugin
gaps:
  - Marketplace catalogs and installation
  - Lifecycle hooks execution
  - Plugin-enabled skill activation
  - Plugin MCP server launch
  - Plugin enable/disable toggling
  - Plugin sharing/distribution
depends_on:
  - mcp-config
  - mcp-client
  - skill
  - skills-framework
---
# Plugin System

## Overview

Jenny implements a plugin system that bundles skills, MCP servers, lifecycle hooks, and app integrations into shareable, versioned packages. The system is modeled after the Codex plugin format (`.codex-plugin/plugin.json`) and provides:

- **Manifest-based discovery**: Plugins are discovered by scanning for `.codex-plugin/plugin.json` files
- **Structured metadata**: Each plugin declares its contents via a JSON manifest
- **Skills integration**: Plugins can bundle skill definitions
- **MCP server integration**: Plugins can include MCP server configurations
- **Lifecycle hooks**: Plugins can define hooks for startup/shutdown events

## Plugin Manifest Format

Plugins use a `.codex-plugin/plugin.json` manifest file located at the plugin root:

```json
{
  "name": "plugin-name",
  "version": "1.2.0",
  "description": "Brief plugin description",
  "author": {
    "name": "Author Name",
    "email": "author@example.com",
    "url": "https://github.com/author"
  },
  "homepage": "https://docs.example.com/plugin",
  "repository": "https://github.com/author/plugin",
  "license": "MIT",
  "keywords": ["keyword1", "keyword2"],
  "skills": "./skills/",
  "hooks": "./hooks.json",
  "mcpServers": "./.mcp.json",
  "apps": "./.app.json",
  "interface": {
    "displayName": "Plugin Display Name",
    "shortDescription": "Short description for subtitle",
    "longDescription": "Long description for details page",
    "developerName": "OpenAI",
    "category": "Productivity",
    "capabilities": ["Interactive", "Write"],
    "brandColor": "#3B82F6"
  }
}
```

### Field Reference

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | Yes | Plugin identifier (kebab-case, no spaces) |
| `version` | string | No | Semantic version (e.g., "1.0.0") |
| `description` | string | No | Brief purpose summary |
| `author` | object | No | Publisher identity |
| `author.name` | string | No | Author or team name |
| `author.email` | string | No | Contact email |
| `author.url` | string | No | Homepage URL |
| `homepage` | string | No | Documentation URL |
| `repository` | string | No | Source code URL |
| `license` | string | No | License identifier (MIT, Apache-2.0, etc.) |
| `keywords` | array | No | Search/discovery tags |
| `skills` | string | No | Relative path to skills directory (must start with `./`) |
| `hooks` | string | No | Relative path to hooks config (must start with `./`) |
| `mcpServers` | string | No | Relative path to MCP config (must start with `./`) |
| `apps` | string | No | Relative path to app manifest (must start with `./`) |
| `interface` | object | No | UX/presentation metadata |

### Interface Fields

| Field | Type | Description |
|-------|------|-------------|
| `displayName` | string | User-facing title shown for the plugin |
| `shortDescription` | string | Brief subtitle used in compact views |
| `longDescription` | string | Longer description used on details screens |
| `developerName` | string | Human-readable publisher name |
| `category` | string | Plugin category bucket (e.g., "Productivity") |
| `capabilities` | array | Capability list (e.g., ["Interactive", "Write"]) |
| `brandColor` | string | Theme color for the plugin card (hex format) |
| `websiteURL` | string | Must start with `https://` |
| `privacyPolicyURL` | string | Must start with `https://` |
| `termsOfServiceURL` | string | Must start with `https://` |

## Plugin Directory Structure

```
<plugin-root>/
├── .codex-plugin/
│   └── plugin.json          # Plugin manifest (required)
├── skills/                  # Skill definitions (optional)
├── .mcp.json                # MCP server config (optional)
├── hooks.json               # Lifecycle hooks config (optional)
└── .app.json                # App integration manifest (optional)
```

## Discovery Algorithm

`FindPluginRoots(rootDir string) []string`:

1. Walk directory tree from `rootDir`
2. Maximum depth: 5 levels from root
3. Skip hidden directories (starting with `.`)
4. When `.codex-plugin/plugin.json` is found:
   - Return the parent directory as a plugin root
   - Skip recursion into the plugin directory
5. Return list of plugin root directories

## Loading and Validation

### ParseManifest

```go
func ParseManifest(data []byte) (*PluginManifest, error)
```

- Returns `nil, nil` for empty input
- Returns error for invalid JSON
- Returns error for missing `name` field

### LoadManifest

```go
func LoadManifest(path string) (*PluginManifest, error)
```

- Reads file from `path`
- Delegates to `ParseManifest`
- Returns error for non-existent file or read failure

### LoadedPlugin.Validate

```go
func (p *LoadedPlugin) Validate() error
```

Validates:
- Manifest is non-nil
- `name` field is non-empty
- Path fields (`skills`, `mcpServers`, `hooks`, `apps`) are empty or start with `./`
- Interface URLs (if set) start with `https://`

## Integration with Existing Systems

### Skills Framework (`internal/skills/`)

Plugins can bundle skills via the `skills` field:

- Path must be relative starting with `./`
- Skills are loaded in addition to default skill discovery
- Integration point: `LoadPluginSkills(pluginRoot)` (future)

### MCP Client (`internal/mcp/`)

Plugins can include MCP server definitions via `mcpServers`:

- Path must be relative starting with `./`
- MCP config format: see [`mcp-config.md`](./mcp-config.md)
- Integration point: `LoadPluginMCPServers(pluginRoot)` (future)

### Hooks (Future)

Plugins can define lifecycle hooks via `hooks`:

```json
{
  "onStartup": "./hooks/startup.sh",
  "onShutdown": "./hooks/shutdown.sh",
  "onSessionStart": "./hooks/session-start.sh",
  "onSessionEnd": "./hooks/session-end.sh"
}
```

Integration point: `ExecutePluginHooks(pluginRoot, hookName)` (future)

## Future Roadmap

### Phase 1: Foundation (This Iteration)
- [x] Manifest types (`PluginManifest`, `AuthorInfo`, `PluginManifestInterface`)
- [x] Manifest parsing (`ParseManifest`, `LoadManifest`)
- [x] Plugin discovery (`FindPluginRoots`)
- [x] Plugin validation (`LoadedPlugin.Validate`)

### Phase 2: Marketplace (Deferred)
- [ ] Marketplace catalog support
- [ ] Plugin installation command
- [ ] Plugin registry/discovery UI

### Phase 3: Integration (Deferred)
- [ ] Plugin skill activation
- [ ] Plugin MCP server launch
- [ ] Lifecycle hook execution
- [ ] Plugin enable/disable toggling

### Phase 4: Distribution (Deferred)
- [ ] Plugin packaging and sharing
- [ ] Plugin marketplace publishing
- [ ] Plugin version management

## Edge Cases

| Case | Expected behavior |
|------|-------------------|
| Empty manifest | Returns `nil, nil` |
| Missing `name` field | Returns error |
| Invalid JSON | Returns error |
| Non-existent file | Returns error |
| Path not starting with `./` | Validation error |
| `http://` URL (not `https://`) | Validation error |
| Hidden plugin directory | Skipped during discovery |
| Max depth exceeded | Skips deeper directories |
| No plugins found | Returns empty slice |

## Headless Protocol Compatibility

- Plugins are discovered from workspace and user config directories
- MCP servers from plugins are surfaced as `mcp__<server>__<tool>` tools
- Plugin metadata exposed in `system`/`init` stream-json line
