// Package agent provides the core agent types and utilities.
package agent

import "strings"

// SubagentType defines a built-in subagent type with distinct tool allowlists,
// models, and resume semantics.
type SubagentType struct {
	Name                    string
	Description             string
	allowedTools            []string
	deniedTools             []string
	model                   string
	oneShot                 bool
	omitProjectInstructions bool
	mcpServers              []string
}

// FilterTools returns a filtered allowlist excluding denied tools.
// If allowedTools contains "*", start with all known tools and subtract denied.
// Otherwise, start with allowedTools and remove any entries in denied or deniedTools.
// Returns a new slice (does not mutate the type).
func (t SubagentType) FilterTools(denied []string) []string {
	deniedMap := make(map[string]bool)
	for _, d := range denied {
		deniedMap[d] = true
	}

	// If allowedTools contains "*", return all tools except denied
	if len(t.allowedTools) == 1 && t.allowedTools[0] == "*" {
		// All known tool names
		allTools := []string{
			"Read", "Write", "Edit", "Bash", "Glob", "Grep",
			"WebSearch", "WebFetch", "LSP", "Skill", "NotebookEdit", "ReadMcpResource",
		}
		var result []string
		for _, tool := range allTools {
			if !deniedMap[tool] {
				result = append(result, tool)
			}
		}
		return result
	}

	// Otherwise, filter from allowedTools
	var result []string
	for _, tool := range t.allowedTools {
		if !deniedMap[tool] {
			result = append(result, tool)
		}
	}
	// Also remove any tools in deniedTools
	for _, d := range t.deniedTools {
		deniedMap[d] = true
	}
	var filtered []string
	for _, tool := range result {
		if !deniedMap[tool] {
			filtered = append(filtered, tool)
		}
	}
	return filtered
}

// CanResume returns whether this subagent type supports resuming a session.
// One-shot types return false.
func (t SubagentType) CanResume() bool {
	return !t.oneShot
}

// AllowedTools returns a copy of the type's allowed tools list.
func (t SubagentType) AllowedTools() []string {
	result := make([]string, len(t.allowedTools))
	copy(result, t.allowedTools)
	return result
}

// RequiredMCPServers returns a copy of the type's required MCP servers list.
func (t SubagentType) RequiredMCPServers() []string {
	result := make([]string, len(t.mcpServers))
	copy(result, t.mcpServers)
	return result
}

// BuiltinTypes returns all five built-in subagent types.
func BuiltinTypes() []SubagentType {
	return []SubagentType{
		GeneralPurpose,
		Explore,
		Plan,
		Shell,
		Verification,
	}
}

// FindBuiltin returns a built-in type by name, or nil if not found.
func FindBuiltin(name string) *SubagentType {
	for _, t := range BuiltinTypes() {
		if t.Name == name {
			return &t
		}
	}
	return nil
}

// GeneralPurpose is the default subagent type with all tools allowed.
var GeneralPurpose = SubagentType{
	Name:                    "general-purpose",
	Description:             "Default subagent for general tasks",
	allowedTools:            []string{"*"},
	deniedTools:             []string{},
	model:                   "inherit",
	oneShot:                 false,
	omitProjectInstructions: false,
	mcpServers:              []string{},
}

// Explore is a read-only subagent type for exploration tasks.
var Explore = SubagentType{
	Name:                    "explore",
	Description:             "Read-only exploration agent for searching and reading files",
	allowedTools:            []string{"Read", "Glob", "Grep", "Bash"},
	deniedTools:             []string{"Write", "Edit", "Agent"},
	model:                   "inherit",
	oneShot:                 true,
	omitProjectInstructions: false,
	mcpServers:              []string{},
}

// Plan is a read-only subagent type for planning tasks.
var Plan = SubagentType{
	Name:                    "plan",
	Description:             "Read-only planning agent for analysis and design",
	allowedTools:            []string{"Read", "Glob", "Grep"},
	deniedTools:             []string{"Write", "Edit", "Bash", "Agent"},
	model:                   "inherit",
	oneShot:                 true,
	omitProjectInstructions: true,
	mcpServers:              []string{},
}

// Shell is a subagent type focused on shell command execution.
var Shell = SubagentType{
	Name:                    "shell",
	Description:             "Shell-focused agent for command execution",
	allowedTools:            []string{"Bash", "Read", "Glob", "Grep"},
	deniedTools:             []string{},
	model:                   "inherit",
	oneShot:                 false,
	omitProjectInstructions: false,
	mcpServers:              []string{},
}

// Verification is a subagent type for CI-style verification tasks.
var Verification = SubagentType{
	Name:                    "verification",
	Description:             "Verification agent for running tests and CI checks",
	allowedTools:            []string{"Read", "Glob", "Grep", "Bash"},
	deniedTools:             []string{"Write", "Edit"},
	model:                   "inherit",
	oneShot:                 false,
	omitProjectInstructions: false,
	mcpServers:              []string{},
}

// modelAliases maps model alias names to concrete model identifiers.
var modelAliases = map[string]string{
	"sonnet": "claude-sonnet-4-20250514",
	"opus":   "claude-opus-4-20250514",
	"haiku":  "claude-haiku-4-20250514",
}

// ResolveModel resolves a model alias to its concrete model identifier.
// If the alias is unknown, returns the input unchanged.
func ResolveModel(alias string) string {
	if resolved, ok := modelAliases[strings.ToLower(alias)]; ok {
		return resolved
	}
	return alias
}
