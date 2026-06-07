// Package mcp provides MCP server configuration loading and management.
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"sync"
	"time"

	"github.com/ipy/jenny/internal/tool"
)

const resourceCacheTTL = 30 * time.Second

// resourceCacheEntry holds cached resource list with timestamp.
type resourceCacheEntry struct {
	resources []MCPResource
	fetchedAt time.Time
}

// ListMcpResourcesTool lists MCP resources from connected servers.
type ListMcpResourcesTool struct {
	cache map[string]*resourceCacheEntry
	mu    sync.Mutex
}

// NewListMcpResourcesTool creates a new ListMcpResourcesTool.
func NewListMcpResourcesTool() *ListMcpResourcesTool {
	return &ListMcpResourcesTool{
		cache: make(map[string]*resourceCacheEntry),
	}
}

// Name returns the tool name.
func (t *ListMcpResourcesTool) Name() string {
	return "list_mcp_resources"
}

// Description returns a description of the tool.
func (t *ListMcpResourcesTool) Description() string {
	return "List MCP resources from connected servers. Returns resources with uri, name, mimeType, description, and server fields."
}

// InputSchema returns the JSON schema for tool input.
func (t *ListMcpResourcesTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"server": map[string]any{
				"type":        "string",
				"description": "Optional MCP server name to filter resources",
			},
		},
	}
}

// Execute lists MCP resources from connected servers.
func (t *ListMcpResourcesTool) Execute(ctx context.Context, input map[string]any, cwd string) (*tool.ToolResult, error) {
	serverFilter, _ := input["server"].(string)

	// AC2: Invalid server errors with available names
	if serverFilter != "" {
		client := GetClient(serverFilter)
		if client == nil {
			// List available server names
			clients := GetMCPClients()
			var available []string
			for name := range clients {
				available = append(available, name)
			}
			return &tool.ToolResult{
				Content: fmt.Sprintf("MCP server '%s' not found. Available servers: %v", serverFilter, available),
				IsError: true,
			}, nil
		}
		// Single server mode
		return t.executeForServer(ctx, serverFilter, client)
	}

	// AC1: No filter returns all connected servers' resources
	return t.executeForAllServers(ctx)
}

// executeForServer returns resources for a single server.
func (t *ListMcpResourcesTool) executeForServer(ctx context.Context, serverName string, client *Client) (*tool.ToolResult, error) {
	resources, err := t.getResourcesWithCache(ctx, serverName, client)
	if err != nil {
		// AC3: Per-server failure returns empty array for that server
		resources = []MCPResource{}
	}

	return t.buildResult(resources, serverName)
}

// executeForAllServers returns resources from all connected servers concurrently.
func (t *ListMcpResourcesTool) executeForAllServers(ctx context.Context) (*tool.ToolResult, error) {
	clients := GetMCPClients()
	if len(clients) == 0 {
		// AC4: Empty result includes tools-may-exist note
		return &tool.ToolResult{
			Content: "[]\nNote: No MCP servers connected. Resources may be empty while tools still exist.",
			IsError: false,
		}, nil
	}

	// Use errgroup for concurrency with small goroutine pool
	type serverResult struct {
		serverName string
		resources  []MCPResource
		err        error
	}

	var results []serverResult
	mu := sync.Mutex{}

	// Limit concurrency to avoid overwhelming the system
	const maxConcurrency = 5
	sem := make(chan struct{}, maxConcurrency)
	var wg sync.WaitGroup

	for name, client := range clients {
		wg.Add(1)
		go func(serverName string, client *Client) {
			defer wg.Done()

			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-sem }()

			resources, err := t.getResourcesWithCache(ctx, serverName, client)
			mu.Lock()
			results = append(results, serverResult{serverName, resources, err})
			mu.Unlock()
		}(name, client)
	}

	wg.Wait()

	// Aggregate results from all servers
	var allResources []MCPResource
	var hasResources bool

	for _, sr := range results {
		if sr.err != nil {
			// AC3: Per-server failure returns [] for that server (not whole-call failure)
			allResources = append(allResources, []MCPResource{}...)
			continue
		}
		allResources = append(allResources, sr.resources...)
		if len(sr.resources) > 0 {
			hasResources = true
		}
	}

	// AC4: Empty result includes tools-may-exist note
	if !hasResources {
		return &tool.ToolResult{
			Content: "[]\nNote: No resources found. Resources may be empty while tools still exist.",
			IsError: false,
		}, nil
	}

	return t.buildResult(allResources, "")
}

// getResourcesWithCache returns cached resources or fetches new ones.
func (t *ListMcpResourcesTool) getResourcesWithCache(ctx context.Context, serverName string, client *Client) ([]MCPResource, error) {
	t.mu.Lock()
	entry, exists := t.cache[serverName]
	t.mu.Unlock()

	if exists && time.Since(entry.fetchedAt) < resourceCacheTTL {
		return entry.resources, nil
	}

	resources, err := client.ListResources(ctx)
	if err != nil {
		return nil, err
	}

	t.mu.Lock()
	t.cache[serverName] = &resourceCacheEntry{
		resources: resources,
		fetchedAt: time.Now(),
	}
	t.mu.Unlock()

	return resources, nil
}

// buildResult constructs the JSON output from resources.
func (t *ListMcpResourcesTool) buildResult(resources []MCPResource, serverName string) (*tool.ToolResult, error) {
	// Build output array with server field
	output := make([]map[string]any, 0, len(resources))
	for _, r := range resources {
		entry := map[string]any{
			"uri":  r.URI,
			"name": r.Name,
		}
		if r.MimeType != "" {
			entry["mimeType"] = r.MimeType
		}
		if r.Description != "" {
			entry["description"] = r.Description
		}
		entry["server"] = serverName
		output = append(output, entry)
	}

	jsonBytes, err := json.Marshal(output)
	if err != nil {
		return &tool.ToolResult{
			Content: fmt.Sprintf("Error marshaling resources: %v", err),
			IsError: true,
		}, nil
	}

	return &tool.ToolResult{
		Content: string(jsonBytes),
		IsError: false,
	}, nil
}

// InvalidateCache clears the resource cache (call on disconnect).
func (t *ListMcpResourcesTool) InvalidateCache() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.cache = make(map[string]*resourceCacheEntry)
}

// Clone returns a deep copy of the cache for snapshot.
func (t *ListMcpResourcesTool) Clone() map[string]*resourceCacheEntry {
	t.mu.Lock()
	defer t.mu.Unlock()
	result := make(map[string]*resourceCacheEntry, len(t.cache))
	maps.Copy(result, t.cache)
	return result
}
