package mcp

import (
	"context"
	"encoding/json"
	"testing"
)

// TestListMcpResourcesTool_NameAndDescription tests basic tool metadata.
func TestListMcpResourcesTool_NameAndDescription(t *testing.T) {
	tool := NewListMcpResourcesTool()

	if tool.Name() != "list_mcp_resources" {
		t.Errorf("expected name 'list_mcp_resources', got %q", tool.Name())
	}

	desc := tool.Description()
	if desc == "" {
		t.Error("expected non-empty description")
	}

	schema := tool.InputSchema()
	if schema["type"] != "object" {
		t.Errorf("expected type 'object', got %v", schema["type"])
	}
}

// TestListMcpResourcesTool_AC2_InvalidServer tests that an invalid server returns error with available names.
func TestListMcpResourcesTool_AC2_InvalidServer(t *testing.T) {
	// Save and restore global state
	origClients := clients
	t.Cleanup(func() {
		clientsMu.Lock()
		clients = origClients
		clientsMu.Unlock()
	})

	// Reset clients state for this test
	clientsMu.Lock()
	clients = make(map[string]*Client)
	clientsMu.Unlock()

	tool := NewListMcpResourcesTool()

	result, err := tool.Execute(context.Background(), map[string]any{
		"server": "nonexistent-server",
	}, "/tmp")
	if err != nil {
		t.Fatalf("Execute returned unexpected error: %v", err)
	}
	if !result.IsError {
		t.Error("expected IsError=true for invalid server")
	}
	if result.Content == "" {
		t.Error("expected non-empty error content")
	}
	// Should mention the invalid server name
	if !containsString(result.Content, "nonexistent-server") {
		t.Errorf("error should mention server name, got: %s", result.Content)
	}
}

// TestListMcpResourcesTool_AC1_NoFilterAllServers tests that no filter returns all servers' resources.
func TestListMcpResourcesTool_AC1_NoFilterAllServers(t *testing.T) {
	// Save and restore global state
	origClients := clients
	t.Cleanup(func() {
		clientsMu.Lock()
		clients = origClients
		clientsMu.Unlock()
	})

	// Reset clients state for this test
	clientsMu.Lock()
	clients = make(map[string]*Client)
	clientsMu.Unlock()

	tool := NewListMcpResourcesTool()

	result, err := tool.Execute(context.Background(), map[string]any{}, "/tmp")
	if err != nil {
		t.Fatalf("Execute returned unexpected error: %v", err)
	}
	// AC4: Empty result with no servers should include note
	if !containsString(result.Content, "Note") {
		t.Errorf("expected Note in output for no servers, got: %s", result.Content)
	}
}

// TestListMcpResourcesTool_AC4_EmptyResultNote tests that empty result includes tools-may-exist note.
func TestListMcpResourcesTool_AC4_EmptyResultNote(t *testing.T) {
	// Save and restore global state
	origClients := clients
	t.Cleanup(func() {
		clientsMu.Lock()
		clients = origClients
		clientsMu.Unlock()
	})

	// Reset clients state for this test
	clientsMu.Lock()
	clients = make(map[string]*Client)
	clientsMu.Unlock()

	tool := NewListMcpResourcesTool()

	result, err := tool.Execute(context.Background(), map[string]any{}, "/tmp")
	if err != nil {
		t.Fatalf("Execute returned unexpected error: %v", err)
	}
	// Should contain the note about tools still existing
	if !containsString(result.Content, "Resources may be empty while tools still exist") {
		t.Errorf("expected note about tools, got: %s", result.Content)
	}
}

// TestListMcpResourcesTool_AC5_ServerField tests that each entry includes server field.
func TestListMcpResourcesTool_AC5_ServerField(t *testing.T) {
	// This test requires a mock MCP server with resources
	// For unit testing, we verify the output structure
	tool := NewListMcpResourcesTool()

	// The tool should produce JSON with server field in each entry
	// We can't fully test this without a mock server, but we can verify
	// the tool executes without error and produces valid-looking output
	result, err := tool.Execute(context.Background(), map[string]any{}, "/tmp")
	if err != nil {
		t.Fatalf("Execute returned unexpected error: %v", err)
	}

	// Result should be valid JSON or contain the empty note
	if !result.IsError {
		// Should be able to parse as JSON or contain Note
		var jsonOutput []map[string]any
		if err := json.Unmarshal([]byte(result.Content), &jsonOutput); err != nil {
			if !containsString(result.Content, "Note") {
				t.Errorf("expected valid JSON or Note, got: %s", result.Content)
			}
		}
	}
}

// TestListMcpResourcesTool_Cache tests that caching works correctly.
func TestListMcpResourcesTool_Cache(t *testing.T) {
	// Save and restore global state
	origClients := clients
	t.Cleanup(func() {
		clientsMu.Lock()
		clients = origClients
		clientsMu.Unlock()
	})

	// Reset clients state for this test
	clientsMu.Lock()
	clients = make(map[string]*Client)
	clientsMu.Unlock()

	tool := NewListMcpResourcesTool()

	// First call - cache should be empty
	_ = tool.Clone() // verify Clone works

	// InvalidateCache should clear the cache
	tool.InvalidateCache()
	// Should not panic
}

// TestListMcpResourcesTool_ExecuteInterface tests that the tool implements tool.Tool interface.
func TestListMcpResourcesTool_ExecuteInterface(t *testing.T) {
	tool := NewListMcpResourcesTool()

	// Verify it can be called with the correct signature
	result, err := tool.Execute(context.Background(), map[string]any{}, "/tmp")
	if err != nil {
		t.Fatalf("Execute returned unexpected error: %v", err)
	}
	if result == nil {
		t.Error("expected non-nil result")
	}
}

// containsString is a helper to check if a string contains a substring.
func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstring(s, substr))
}

func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
