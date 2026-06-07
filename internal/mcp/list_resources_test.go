package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
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
	if !strings.Contains(result.Content, "nonexistent-server") {
		t.Errorf("error should mention server name, got: %s", result.Content)
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
	if !strings.Contains(result.Content, "Resources may be empty while tools still exist") {
		t.Errorf("expected note about tools, got: %s", result.Content)
	}
}

// TestListMcpResourcesTool_AC1_MultiServerAggregation tests AC1: no filter returns all servers' resources.
func TestListMcpResourcesTool_AC1_MultiServerAggregation(t *testing.T) {
	// Save and restore global state
	origClients := clients
	origHook := listResourcesHook
	t.Cleanup(func() {
		clientsMu.Lock()
		clients = origClients
		clientsMu.Unlock()
		listResourcesHook = origHook
	})

	// Reset clients state for this test
	clientsMu.Lock()
	clients = make(map[string]*Client)
	clientsMu.Unlock()

	// Register two mock clients
	clients["server1"] = &Client{Name: "server1"}
	clients["server2"] = &Client{Name: "server2"}

	// Set up test hook to return different resources per server
	listResourcesHook = func(ctx context.Context, serverName string) ([]MCPResource, error) {
		if serverName == "server1" {
			return []MCPResource{
				{URI: "file:///a.txt", Name: "a.txt", MimeType: "text/plain", Description: "File A"},
			}, nil
		}
		if serverName == "server2" {
			return []MCPResource{
				{URI: "file:///b.txt", Name: "b.txt", MimeType: "text/plain", Description: "File B"},
			}, nil
		}
		return nil, fmt.Errorf("unknown server: %s", serverName)
	}

	tool := NewListMcpResourcesTool()
	result, err := tool.Execute(context.Background(), map[string]any{}, "/tmp")
	if err != nil {
		t.Fatalf("Execute returned unexpected error: %v", err)
	}
	if result.IsError {
		t.Errorf("expected no error, got: %s", result.Content)
	}

	// Parse the JSON output
	var resources []map[string]any
	if err := json.Unmarshal([]byte(result.Content), &resources); err != nil {
		t.Fatalf("failed to parse JSON: %v\ncontent: %s", err, result.Content)
	}

	// Should have 2 resources from both servers
	if len(resources) != 2 {
		t.Errorf("expected 2 resources, got %d: %s", len(resources), result.Content)
	}
}

// TestListMcpResourcesTool_AC3_PartialFailure tests AC3: one server failure returns partial results.
func TestListMcpResourcesTool_AC3_PartialFailure(t *testing.T) {
	// Save and restore global state
	origClients := clients
	origHook := listResourcesHook
	t.Cleanup(func() {
		clientsMu.Lock()
		clients = origClients
		clientsMu.Unlock()
		listResourcesHook = origHook
	})

	// Reset clients state for this test
	clientsMu.Lock()
	clients = make(map[string]*Client)
	clientsMu.Unlock()

	// Register two mock clients
	clients["good-server"] = &Client{Name: "good-server"}
	clients["bad-server"] = &Client{Name: "bad-server"}

	// Set up test hook where one server fails
	listResourcesHook = func(ctx context.Context, serverName string) ([]MCPResource, error) {
		if serverName == "bad-server" {
			return nil, fmt.Errorf("connection refused")
		}
		if serverName == "good-server" {
			return []MCPResource{
				{URI: "file:///good.txt", Name: "good.txt", MimeType: "text/plain"},
			}, nil
		}
		return nil, fmt.Errorf("unknown server: %s", serverName)
	}

	tool := NewListMcpResourcesTool()
	result, err := tool.Execute(context.Background(), map[string]any{}, "/tmp")
	if err != nil {
		t.Fatalf("Execute returned unexpected error: %v", err)
	}
	// Should not be an error overall (partial failure tolerance)
	if result.IsError {
		t.Errorf("expected no error for partial failure, got: %s", result.Content)
	}

	// Parse the JSON output - should have errors map and resources
	var output struct {
		Resources []map[string]any  `json:"resources"`
		Errors    map[string]string `json:"errors"`
	}
	if err := json.Unmarshal([]byte(result.Content), &output); err != nil {
		t.Fatalf("failed to parse JSON: %v\ncontent: %s", err, result.Content)
	}

	// Should have 1 resource from good server
	if len(output.Resources) != 1 {
		t.Errorf("expected 1 resource from good server, got %d: %s", len(output.Resources), result.Content)
	}

	// Should have an errors map with the bad server
	if output.Errors == nil {
		t.Error("expected errors map for failed server")
	}
	if _, ok := output.Errors["bad-server"]; !ok {
		t.Errorf("expected error entry for bad-server, got: %+v", output.Errors)
	}
}

// TestListMcpResourcesTool_AC5_ServerFieldPerEntry tests AC5: each entry includes server field.
func TestListMcpResourcesTool_AC5_ServerFieldPerEntry(t *testing.T) {
	// Save and restore global state
	origClients := clients
	origHook := listResourcesHook
	t.Cleanup(func() {
		clientsMu.Lock()
		clients = origClients
		clientsMu.Unlock()
		listResourcesHook = origHook
	})

	// Reset clients state for this test
	clientsMu.Lock()
	clients = make(map[string]*Client)
	clientsMu.Unlock()

	// Register two mock clients
	clients["server-a"] = &Client{Name: "server-a"}
	clients["server-b"] = &Client{Name: "server-b"}

	// Set up test hook to return different resources per server
	listResourcesHook = func(ctx context.Context, serverName string) ([]MCPResource, error) {
		if serverName == "server-a" {
			return []MCPResource{
				{URI: "file:///a1.txt", Name: "a1.txt"},
				{URI: "file:///a2.txt", Name: "a2.txt"},
			}, nil
		}
		if serverName == "server-b" {
			return []MCPResource{
				{URI: "file:///b1.txt", Name: "b1.txt"},
			}, nil
		}
		return nil, fmt.Errorf("unknown server: %s", serverName)
	}

	tool := NewListMcpResourcesTool()
	result, err := tool.Execute(context.Background(), map[string]any{}, "/tmp")
	if err != nil {
		t.Fatalf("Execute returned unexpected error: %v", err)
	}
	if result.IsError {
		t.Errorf("expected no error, got: %s", result.Content)
	}

	// Parse the JSON output
	var resources []map[string]any
	if err := json.Unmarshal([]byte(result.Content), &resources); err != nil {
		t.Fatalf("failed to parse JSON: %v\ncontent: %s", err, result.Content)
	}

	// Build a map of server -> resource names
	serverToResources := make(map[string][]string)
	for _, r := range resources {
		server, ok := r["server"].(string)
		if !ok {
			t.Errorf("resource missing or invalid server field: %+v", r)
			continue
		}
		name, ok := r["name"].(string)
		if !ok {
			continue
		}
		serverToResources[server] = append(serverToResources[server], name)
	}

	// server-a should have a1.txt and a2.txt
	if len(serverToResources["server-a"]) != 2 {
		t.Errorf("expected 2 resources from server-a, got: %v", serverToResources["server-a"])
	}

	// server-b should have b1.txt
	if len(serverToResources["server-b"]) != 1 {
		t.Errorf("expected 1 resource from server-b, got: %v", serverToResources["server-b"])
	}
}

// TestListMcpResourcesTool_Cache tests that caching works correctly.
func TestListMcpResourcesTool_Cache(t *testing.T) {
	// Save and restore global state
	origClients := clients
	origHook := listResourcesHook
	t.Cleanup(func() {
		clientsMu.Lock()
		clients = origClients
		clientsMu.Unlock()
		listResourcesHook = origHook
	})

	// Reset clients state for this test
	clientsMu.Lock()
	clients = make(map[string]*Client)
	clientsMu.Unlock()

	clients["test-server"] = &Client{Name: "test-server"}

	callCount := 0
	listResourcesHook = func(ctx context.Context, serverName string) ([]MCPResource, error) {
		callCount++
		return []MCPResource{{URI: "file:///test.txt", Name: "test.txt"}}, nil
	}

	tool := NewListMcpResourcesTool()

	// First call - should fetch
	_, _ = tool.Execute(context.Background(), map[string]any{}, "/tmp")
	if callCount != 1 {
		t.Errorf("expected 1 call on first execution, got %d", callCount)
	}

	// Second call - should use cache (within TTL)
	_, _ = tool.Execute(context.Background(), map[string]any{}, "/tmp")
	if callCount != 1 {
		t.Errorf("expected 1 call (cached), got %d", callCount)
	}

	// Invalidate cache
	tool.InvalidateCache()

	// Third call - should fetch again
	_, _ = tool.Execute(context.Background(), map[string]any{}, "/tmp")
	if callCount != 2 {
		t.Errorf("expected 2 calls after cache invalidation, got %d", callCount)
	}
}

// TestListMcpResourcesTool_ExecuteInterface tests that the tool implements tool.Tool interface.
func TestListMcpResourcesTool_ExecuteInterface(t *testing.T) {
	// Save and restore global state
	origClients := clients
	origHook := listResourcesHook
	t.Cleanup(func() {
		clientsMu.Lock()
		clients = origClients
		clientsMu.Unlock()
		listResourcesHook = origHook
	})

	// Reset clients state for this test
	clientsMu.Lock()
	clients = make(map[string]*Client)
	clientsMu.Unlock()

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
