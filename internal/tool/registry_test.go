package tool

import (
	"testing"
)

// mockTool implements Tool for testing.
type mockTool struct {
	name string
}

func (t *mockTool) Name() string                { return t.name }
func (t *mockTool) Description() string         { return "mock tool " + t.name }
func (t *mockTool) InputSchema() map[string]any { return map[string]any{"type": "object"} }
func (t *mockTool) Execute(map[string]any, string) (*ToolResult, error) {
	return &ToolResult{Content: "executed " + t.name}, nil
}

func TestRegistry_WithBaseTools(t *testing.T) {
	tools := NewRegistry().WithBaseTools().Build()

	if len(tools) != 2 {
		t.Errorf("expected 2 base tools, got %d", len(tools))
	}

	names := make(map[string]bool)
	for _, t := range tools {
		names[t.Name()] = true
	}

	if !names["read"] {
		t.Error("expected 'read' tool")
	}
	if !names["bash"] {
		t.Error("expected 'bash' tool")
	}
}

func TestRegistry_WithDenyRules(t *testing.T) {
	tools := NewRegistry().
		WithBaseTools().
		WithDenyRules([]string{"read"}).
		Build()

	if len(tools) != 1 {
		t.Errorf("expected 1 tool after denying 'read', got %d", len(tools))
	}

	if len(tools) > 0 && tools[0].Name() != "bash" {
		t.Errorf("expected remaining tool to be 'bash', got %q", tools[0].Name())
	}
}

func TestRegistry_DenyRules_NonExistent(t *testing.T) {
	tools := NewRegistry().
		WithBaseTools().
		WithDenyRules([]string{"nonexistent"}).
		Build()

	// Denying a non-existent tool should be a no-op
	if len(tools) != 2 {
		t.Errorf("expected 2 tools when denying non-existent, got %d", len(tools))
	}
}

func TestRegistry_WithMCPTools(t *testing.T) {
	mcpTools := []Tool{
		&mockTool{name: "mcp__server__tool1"},
		&mockTool{name: "mcp__server__tool2"},
	}

	tools := NewRegistry().
		WithBaseTools().
		WithMCPTools(mcpTools).
		Build()

	if len(tools) != 4 {
		t.Errorf("expected 4 tools (2 base + 2 MCP), got %d", len(tools))
	}

	// Base tools should come first
	if tools[0].Name() != "read" {
		t.Errorf("expected first tool to be 'read', got %q", tools[0].Name())
	}
	if tools[1].Name() != "bash" {
		t.Errorf("expected second tool to be 'bash', got %q", tools[1].Name())
	}

	// MCP tools should come after
	if tools[2].Name() != "mcp__server__tool1" {
		t.Errorf("expected third tool to be 'mcp__server__tool1', got %q", tools[2].Name())
	}
	if tools[3].Name() != "mcp__server__tool2" {
		t.Errorf("expected fourth tool to be 'mcp__server__tool2', got %q", tools[3].Name())
	}
}

func TestRegistry_BuiltInWins(t *testing.T) {
	// If a built-in and MCP tool share a name, built-in wins
	mcpTools := []Tool{
		&mockTool{name: "read"}, // Same name as base tool
	}

	tools := NewRegistry().
		WithBaseTools().
		WithMCPTools(mcpTools).
		Build()

	if len(tools) != 2 {
		t.Errorf("expected 2 tools (built-in takes precedence), got %d", len(tools))
	}

	// First tool should still be the built-in read
	if tools[0].Name() != "read" {
		t.Errorf("expected first tool to be built-in 'read', got %q", tools[0].Name())
	}
}

func TestRegistry_WithEnabled(t *testing.T) {
	tools := NewRegistry().
		WithBaseTools().
		WithEnabled("bash", false).
		Build()

	if len(tools) != 1 {
		t.Errorf("expected 1 tool after disabling 'bash', got %d", len(tools))
	}

	if len(tools) > 0 && tools[0].Name() != "read" {
		t.Errorf("expected remaining tool to be 'read', got %q", tools[0].Name())
	}
}

func TestRegistry_WithEnabled_NotDisabled(t *testing.T) {
	tools := NewRegistry().
		WithBaseTools().
		WithEnabled("bash", true). // Explicitly enabled (default anyway)
		Build()

	if len(tools) != 2 {
		t.Errorf("expected 2 tools, got %d", len(tools))
	}
}

func TestRegistry_EmptyDenyList(t *testing.T) {
	tools := NewRegistry().
		WithBaseTools().
		WithDenyRules([]string{}).
		Build()

	if len(tools) != 2 {
		t.Errorf("expected 2 tools with empty deny list, got %d", len(tools))
	}
}

func TestRegistry_MCPToolsOnly(t *testing.T) {
	mcpTools := []Tool{
		&mockTool{name: "mcp__server__tool1"},
	}

	tools := NewRegistry().
		WithMCPTools(mcpTools).
		Build()

	if len(tools) != 1 {
		t.Errorf("expected 1 MCP tool, got %d", len(tools))
	}
}

func TestRegistry_NoTools(t *testing.T) {
	tools := NewRegistry().Build()

	if len(tools) != 0 {
		t.Errorf("expected 0 tools, got %d", len(tools))
	}
}

func TestRegistry_DenyMCPTool(t *testing.T) {
	mcpTools := []Tool{
		&mockTool{name: "mcp__server__tool1"},
		&mockTool{name: "mcp__server__tool2"},
	}

	tools := NewRegistry().
		WithBaseTools().
		WithMCPTools(mcpTools).
		WithDenyRules([]string{"mcp__server__tool1"}).
		Build()

	if len(tools) != 3 {
		t.Errorf("expected 3 tools (2 base + 1 MCP), got %d", len(tools))
	}
}

func TestRegistry_CombinedFilters(t *testing.T) {
	mcpTools := []Tool{
		&mockTool{name: "mcp__server__tool1"},
		&mockTool{name: "mcp__server__tool2"},
	}

	tools := NewRegistry().
		WithBaseTools().
		WithMCPTools(mcpTools).
		WithDenyRules([]string{"read"}).
		WithEnabled("bash", false).
		Build()

	if len(tools) != 2 {
		t.Errorf("expected 2 tools, got %d", len(tools))
	}

	// Should only have bash and one MCP tool
	names := make(map[string]bool)
	for _, t := range tools {
		names[t.Name()] = true
	}

	if names["read"] {
		t.Error("'read' should have been denied")
	}
	if names["bash"] {
		t.Error("'bash' should have been disabled")
	}
}
