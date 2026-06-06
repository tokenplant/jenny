// Package tool provides the tool interface and implementations for the agent.
package tool

// ToolResult represents the result of a tool execution.
type ToolResult struct {
	// Content is the text content of the tool result.
	Content string `json:"content"`
	// IsError indicates whether the tool execution resulted in an error.
	IsError bool `json:"is_error,omitempty"`
}

// Tool defines the interface for agent tools.
type Tool interface {
	// Name returns the tool's name.
	Name() string
	// Description returns a description of the tool for the model.
	Description() string
	// InputSchema returns the JSON schema for tool input.
	InputSchema() map[string]any
	// Execute runs the tool with the given input and returns the result.
	Execute(input map[string]any, cwd string) (*ToolResult, error)
}

// ToolUse represents a tool use request from the model.
type ToolUse struct {
	ID   string         `json:"id"`
	Name string         `json:"name"`
	Args map[string]any `json:"input"`
}

// FindTool finds a tool by name from a list of tools.
func FindTool(tools []Tool, name string) Tool {
	for _, t := range tools {
		if t.Name() == name {
			return t
		}
	}
	return nil
}
