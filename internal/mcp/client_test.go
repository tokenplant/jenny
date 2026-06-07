package mcp

import (
	"context"
	"encoding/json"
	"testing"
)

func TestNormalizeName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"My Server", "my_server"},
		{"list-files", "list_files"},
		{"tool__name", "tool_name"}, // consecutive underscores collapsed
		{"Tool Name", "tool_name"},
		{"server1", "server1"},
		{"my-server-tool", "my_server_tool"},
		{"  spaces  ", "spaces"},
		{"UPPERCASE", "uppercase"},
		{"MiXeD CaSe", "mixed_case"}, // lowercase only, spaces to underscore
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := NormalizeName(tt.input)
			if result != tt.expected {
				t.Errorf("NormalizeName(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestClientConnectAndDisconnect(t *testing.T) {
	// Test that a bad command produces an error
	client := NewClient("test", "nonexistent-command", []string{}, nil)
	err := client.Connect(context.Background())
	if err == nil {
		client.Disconnect()
		t.Error("expected error connecting to nonexistent command, got nil")
	}
}

func TestClientInitialization(t *testing.T) {
	// This test requires a real MCP server or a mock
	// For unit testing, we test the JSON-RPC message format
	req := jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "initialize",
		Params: map[string]any{
			"protocolVersion": "2025-03-26",
			"capabilities":    map[string]any{},
			"clientInfo": map[string]any{
				"name":    "jenny",
				"version": "0.1.0",
			},
		},
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("failed to marshal initialize request: %v", err)
	}

	var parsed jsonRPCRequest
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("failed to unmarshal initialize request: %v", err)
	}

	if parsed.JSONRPC != "2.0" {
		t.Errorf("expected JSONRPC 2.0, got %s", parsed.JSONRPC)
	}
	if parsed.Method != "initialize" {
		t.Errorf("expected method initialize, got %s", parsed.Method)
	}
}

func TestJSONIDGeneration(t *testing.T) {
	// Test that JSON IDs are unique and incrementing
	id1 := nextJSONID()
	id2 := nextJSONID()
	id3 := nextJSONID()

	if id2 <= id1 {
		t.Errorf("id2 should be greater than id1: id1=%d, id2=%d", id1, id2)
	}
	if id3 <= id2 {
		t.Errorf("id3 should be greater than id2: id2=%d, id3=%d", id2, id3)
	}
}

func TestConnectAllWithEmptyConfig(t *testing.T) {
	// Test that ConnectAll handles empty config gracefully
	err := ConnectAll(map[string]MCPServerDef{})
	if err != nil {
		t.Errorf("ConnectAll({}) should not return error, got: %v", err)
	}
}

func TestShutdownAll(t *testing.T) {
	// ShutdownAll should not panic with no clients
	ShutdownAll()
}

func TestGetToolsWithNoClients(t *testing.T) {
	// Reset clients state
	clientsMu.Lock()
	clients = make(map[string]*Client)
	clientsMu.Unlock()

	tools := GetTools()
	if len(tools) != 0 {
		t.Errorf("expected no tools with no clients, got %d", len(tools))
	}
}

func TestGetClientNotFound(t *testing.T) {
	// Reset clients state
	clientsMu.Lock()
	clients = make(map[string]*Client)
	clientsMu.Unlock()

	client := GetClient("nonexistent-server")
	if client != nil {
		t.Error("expected nil for nonexistent server")
	}
}

func TestMCPToolInterface(t *testing.T) {
	mcpTool := &MCPTool{
		serverName:  "My Server",
		toolName:    "List Files",
		inputSchema: map[string]any{"type": "object"},
	}

	// Verify tool name format
	if mcpTool.Name() != "mcp__my_server__list_files" {
		t.Errorf("expected tool name 'mcp__my_server__list_files', got %s", mcpTool.Name())
	}

	// Verify description
	desc := mcpTool.Description()
	if desc == "" {
		t.Error("expected non-empty description")
	}

	// Verify input schema
	schema := mcpTool.InputSchema()
	if schema["type"] != "object" {
		t.Errorf("expected type 'object', got %v", schema["type"])
	}
}

func TestNewClient(t *testing.T) {
	client := NewClient("test-server", "/usr/bin/my-mcp-server", []string{"--flag"}, map[string]string{"KEY": "value"})

	if client.Name != "test-server" {
		t.Errorf("expected name 'test-server', got %s", client.Name)
	}
	if client.cmd != "/usr/bin/my-mcp-server" {
		t.Errorf("expected cmd '/usr/bin/my-mcp-server', got %s", client.cmd)
	}
	if len(client.args) != 1 || client.args[0] != "--flag" {
		t.Errorf("expected args ['--flag'], got %v", client.args)
	}
	if client.env["KEY"] != "value" {
		t.Errorf("expected env KEY='value', got %v", client.env)
	}
}
