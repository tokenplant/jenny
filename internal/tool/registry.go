// Package tool provides the tool interface, implementations, and registry.
package tool

import "github.com/ipy/jenny/internal/sandbox"

// Registry builds a filtered, ordered list of tools.
type Registry struct {
	baseTools        []Tool
	mcpTools         []Tool
	denyRules        map[string]bool
	enabled          map[string]bool
	skipPermissions  bool
	hasBaseTools     bool
	readCache        *ReadFileCache
	sandbox          sandbox.SandboxManager
	webFetchEnabled  bool
	webSearchEnabled bool
	model            string
}

// NewRegistry creates a new Registry.
func NewRegistry() *Registry {
	return &Registry{
		denyRules: make(map[string]bool),
		enabled:   make(map[string]bool),
	}
}

// WithBaseTools registers the canonical base tools (Read, Bash, Glob, Grep).
func (r *Registry) WithBaseTools() *Registry {
	r.hasBaseTools = true
	return r
}

// WithReadFileCache enables the read-before-write cache for Read and Write tools.
// If cache is nil, a new cache is created. The cache is passed through to tools
// that support read-before-write enforcement (Read, Write, Edit, NotebookEdit).
func (r *Registry) WithReadFileCache(cache *ReadFileCache) *Registry {
	if cache == nil {
		cache = NewReadFileCache()
	}
	r.readCache = cache
	return r
}

// WithDenyRules excludes tools by name.
func (r *Registry) WithDenyRules(names []string) *Registry {
	for _, name := range names {
		r.denyRules[name] = true
	}
	return r
}

// WithMCPTools adds MCP tools to the registry.
func (r *Registry) WithMCPTools(tools []Tool) *Registry {
	r.mcpTools = tools
	return r
}

// WithEnabled sets the enabled flag for a tool.
func (r *Registry) WithEnabled(name string, enabled bool) *Registry {
	r.enabled[name] = enabled
	return r
}

// WithSkipPermissions sets the skipPermissions flag for all tools.
func (r *Registry) WithSkipPermissions(skip bool) *Registry {
	r.skipPermissions = skip
	return r
}

// WithSandbox sets the sandbox manager for Bash and Grep tools.
func (r *Registry) WithSandbox(sb sandbox.SandboxManager) *Registry {
	r.sandbox = sb
	return r
}

// WithWebFetchEnabled enables or disables the WebFetch tool.
func (r *Registry) WithWebFetchEnabled(enabled bool) *Registry {
	r.webFetchEnabled = enabled
	return r
}

// WithWebSearchEnabled enables or disables the WebSearch tool.
func (r *Registry) WithWebSearchEnabled(enabled bool) *Registry {
	r.webSearchEnabled = enabled
	return r
}

// WithModel sets the model name for tools that need it (e.g., WebSearch).
func (r *Registry) WithModel(model string) *Registry {
	r.model = model
	return r
}

// Build returns the final ordered tool list.
// Built-in tools appear first, then MCP tools. Deny rules and enabled flags
// filter the output. On name collision, the built-in tool wins.
func (r *Registry) Build() []Tool {
	seen := make(map[string]int) // name -> index
	var result []Tool

	// Create base tools with skipPermissions if hasBaseTools is set
	if r.hasBaseTools {
		r.baseTools = []Tool{
			NewReadTool(r.skipPermissions, r.readCache),
			NewBashTool(r.skipPermissions),
			NewGlobTool(),
			NewGrepTool(),
		}
		// Add WriteTool and EditTool if readCache is configured
		if r.readCache != nil {
			r.baseTools = append(r.baseTools, NewWriteTool(r.readCache), NewEditTool(r.readCache), NewNotebookEditTool(r.readCache))
		}

		// Wire sandbox to BashTool and GrepTool if configured
		if r.sandbox != nil {
			for _, t := range r.baseTools {
				switch tool := t.(type) {
				case *BashTool:
					tool.WithSandbox(r.sandbox)
				case *GrepTool:
					tool.WithSandbox(r.sandbox)
				}
			}
		}

		// Add WebFetch tool if enabled (P3).
		if r.webFetchEnabled {
			r.baseTools = append(r.baseTools, NewWebFetchTool())
		}

		// Add WebSearch tool if enabled (P3).
		if r.webSearchEnabled {
			r.baseTools = append(r.baseTools, NewWebSearchTool(r.model))
		}
	}

	// Add base tools first
	for _, t := range r.baseTools {
		name := t.Name()
		if r.denyRules[name] {
			continue
		}
		if enabled, ok := r.enabled[name]; ok && !enabled {
			continue
		}
		seen[name] = len(result)
		result = append(result, t)
	}

	// Add MCP tools, skipping those that collide with built-ins
	for _, t := range r.mcpTools {
		name := t.Name()
		if r.denyRules[name] {
			continue
		}
		if enabled, ok := r.enabled[name]; ok && !enabled {
			continue
		}
		if _, exists := seen[name]; exists {
			continue // built-in wins
		}
		seen[name] = len(result)
		result = append(result, t)
	}

	return result
}
