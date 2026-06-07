// Package tool provides the tool interface, implementations, and registry.
package tool

// Registry builds a filtered, ordered list of tools.
type Registry struct {
	baseTools []Tool
	mcpTools  []Tool
	denyRules map[string]bool
	enabled   map[string]bool
}

// NewRegistry creates a new Registry.
func NewRegistry() *Registry {
	return &Registry{
		denyRules: make(map[string]bool),
		enabled:   make(map[string]bool),
	}
}

// WithBaseTools registers the canonical base tools (Read, Bash).
func (r *Registry) WithBaseTools() *Registry {
	r.baseTools = []Tool{
		NewReadTool(),
		NewBashTool(),
	}
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

// Build returns the final ordered tool list.
// Built-in tools appear first, then MCP tools. Deny rules and enabled flags
// filter the output. On name collision, the built-in tool wins.
func (r *Registry) Build() []Tool {
	seen := make(map[string]int) // name -> index
	var result []Tool

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
