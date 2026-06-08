// Package tool provides the tool interface, implementations, and registry.
package tool

import (
	"github.com/ipy/jenny/internal/lsp"
	"github.com/ipy/jenny/internal/sandbox"
	"github.com/ipy/jenny/internal/skills"
)

// Registry builds a filtered, ordered list of tools.
type Registry struct {
	baseTools              []Tool
	mcpTools               []Tool
	denyRules              map[string]bool
	enabled                map[string]bool
	skipPermissions        bool
	hasBaseTools           bool
	readCache              *ReadFileCache
	sandbox                sandbox.SandboxManager
	webFetchEnabled        bool
	webSearchEnabled       bool
	lspEnabled             bool
	lspClient              *lsp.Client
	model                  string
	skills                 []skills.Skill
	taskStopEnabled        bool
	todoWriteEnabled       bool
	taskOutputEnabled      bool
	skillsFrameworkEnabled bool
	skillActivator         SkillActivator
	enterWorktreeEnabled   bool
	exitWorktreeEnabled    bool
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

// WithLSPEnabled enables or disables the LSP tool.
func (r *Registry) WithLSPEnabled(enabled bool) *Registry {
	r.lspEnabled = enabled
	return r
}

// WithLSPClient sets the LSP client for the LSP tool.
func (r *Registry) WithLSPClient(client lsp.Client) *Registry {
	r.lspClient = &client
	return r
}

// WithSkills sets the discovered skills for the skill tool.
func (r *Registry) WithSkills(skills []skills.Skill) *Registry {
	r.skills = skills
	return r
}

// WithSkillsFrameworkEnabled enables the skills framework with path-triggered activation.
// When enabled, Read/Write/Edit tools will automatically activate skills based on file paths.
// The activator is wired into Read/Write/Edit tools, and the Skill tool is registered.
func (r *Registry) WithSkillsFrameworkEnabled(enabled bool, skillList []skills.Skill) *Registry {
	r.skillsFrameworkEnabled = enabled
	if enabled && len(skillList) > 0 {
		r.skills = skillList
		r.skillActivator = skills.NewPathSkillActivator(skillList)
	}
	return r
}

// WithTaskStopEnabled enables the TaskStop tool for canceling background tasks.
func (r *Registry) WithTaskStopEnabled(enabled bool) *Registry {
	r.taskStopEnabled = enabled
	return r
}

// WithTodoWriteEnabled enables the TodoWrite tool for in-session todo tracking.
func (r *Registry) WithTodoWriteEnabled(enabled bool) *Registry {
	r.todoWriteEnabled = enabled
	return r
}

// WithTaskOutputEnabled enables the TaskOutput tool for retrieving background task output.
func (r *Registry) WithTaskOutputEnabled(enabled bool) *Registry {
	r.taskOutputEnabled = enabled
	return r
}

// WithEnterWorktreeEnabled enables the EnterWorktree tool for creating isolated git worktree sessions.
func (r *Registry) WithEnterWorktreeEnabled(enabled bool) *Registry {
	r.enterWorktreeEnabled = enabled
	return r
}

// WithExitWorktreeEnabled enables the ExitWorktree tool for exiting git worktree sessions.
func (r *Registry) WithExitWorktreeEnabled(enabled bool) *Registry {
	r.exitWorktreeEnabled = enabled
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

		// Wire skill activator to Read/Write/Edit tools if skills framework is enabled
		if r.skillsFrameworkEnabled && r.skillActivator != nil {
			for _, t := range r.baseTools {
				switch tool := t.(type) {
				case *ReadTool:
					tool.WithSkillActivator(r.skillActivator)
				case *WriteTool:
					tool.WithSkillActivator(r.skillActivator)
				case *EditTool:
					tool.WithSkillActivator(r.skillActivator)
				}
			}
		}

		// Wire TaskManager to BashTool and add TaskStop/TaskOutput tools if enabled
		if r.taskStopEnabled || r.taskOutputEnabled {
			taskManager := NewTaskManager()
			for _, t := range r.baseTools {
				if bt, ok := t.(*BashTool); ok {
					bt.WithTaskManager(taskManager)
					break
				}
			}
			if r.taskStopEnabled {
				r.baseTools = append(r.baseTools, NewTaskStopTool(taskManager))
			}
			if r.taskOutputEnabled {
				r.baseTools = append(r.baseTools, NewTaskOutputTool(taskManager))
			}
		}

		// Add TodoWrite tool if enabled (P4).
		if r.todoWriteEnabled {
			r.baseTools = append(r.baseTools, NewTodoWriteTool())
		}

		// Add WebFetch tool if enabled (P3).
		if r.webFetchEnabled {
			r.baseTools = append(r.baseTools, NewWebFetchTool())
		}

		// Add WebSearch tool if enabled (P3).
		if r.webSearchEnabled {
			r.baseTools = append(r.baseTools, NewWebSearchTool(r.model))
		}

		// Add LSP tool if enabled and client is provided (P3).
		if r.lspEnabled && r.lspClient != nil {
			r.baseTools = append(r.baseTools, NewLSPTool(*r.lspClient))
		}

		// Add Skill tool if skills are discovered (P3).
		if len(r.skills) > 0 {
			r.baseTools = append(r.baseTools, NewSkillTool(r.skills))
		}

		// Add EnterWorktree and ExitWorktree tools if enabled (P4).
		if r.enterWorktreeEnabled {
			r.baseTools = append(r.baseTools, NewEnterWorktreeTool())
		}
		if r.exitWorktreeEnabled {
			r.baseTools = append(r.baseTools, NewExitWorktreeTool())
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
