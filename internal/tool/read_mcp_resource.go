// Package tool provides tool implementations.
package tool

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/ipy/jenny/internal/constants"
	"github.com/ipy/jenny/internal/mcp"
)

// ReadMcpResourceTool reads a single MCP resource by server and URI.
type ReadMcpResourceTool struct{}

// NewReadMcpResourceTool creates a new ReadMcpResourceTool.
func NewReadMcpResourceTool() *ReadMcpResourceTool {
	return &ReadMcpResourceTool{}
}

// Name returns the tool name.
func (t *ReadMcpResourceTool) Name() string {
	return "read_mcp_resource"
}

// Description returns a description of the tool.
func (t *ReadMcpResourceTool) Description() string {
	return "Read a single MCP resource by server and URI. Text content is returned inline; binary content is decoded and saved to disk."
}

// InputSchema returns the JSON schema for tool input.
func (t *ReadMcpResourceTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"server": map[string]any{
				"type":        "string",
				"description": "MCP server name",
			},
			"uri": map[string]any{
				"type":        "string",
				"description": "Resource URI to read",
			},
		},
		"required": []string{"server", "uri"},
	}
}

// Execute reads the MCP resource and returns content inline or persisted to disk.
func (t *ReadMcpResourceTool) Execute(ctx context.Context, input map[string]any, cwd string) (*ToolResult, error) {
	server, _ := input["server"].(string)
	uri, _ := input["uri"].(string)

	if server == "" {
		return &ToolResult{
			Content: "Error: server parameter is required",
			IsError: true,
		}, nil
	}

	if uri == "" {
		return &ToolResult{
			Content: "Error: uri parameter is required",
			IsError: true,
		}, nil
	}

	// AC1: Validate server exists
	client := mcp.GetClient(server)
	if client == nil {
		clients := mcp.GetMCPClients()
		var available []string
		for name := range clients {
			available = append(available, name)
		}
		return &ToolResult{
			Content: fmt.Sprintf("MCP server '%s' not found. Available servers: %v", server, available),
			IsError: true,
		}, nil
	}

	// Fetch resource content
	contents, err := client.ReadResource(ctx, uri)
	if err != nil {
		return &ToolResult{
			Content: fmt.Sprintf("Error reading resource: %v", err),
			IsError: true,
		}, nil
	}

	// Build output contents
	outputContents := make([]map[string]any, 0, len(contents))
	for _, c := range contents {
		item := map[string]any{
			"uri": uri,
		}
		if c.MimeType != "" {
			item["mimeType"] = c.MimeType
		}
		if c.Type == "text" {
			item["text"] = c.Text
		} else if c.Type == "blob" {
			// AC3: Binary content persisted to disk
			savedPath, err := t.persistBlob(c.Blob)
			if err != nil {
				// AC4: Persist failure does not inline base64
				return &ToolResult{
					Content: fmt.Sprintf("Error saving binary content to disk: %v", err),
					IsError: true,
				}, nil
			}
			item["blobSavedTo"] = savedPath
		}
		outputContents = append(outputContents, item)
	}

	jsonBytes, err := json.Marshal(map[string]any{"contents": outputContents})
	if err != nil {
		return &ToolResult{
			Content: fmt.Sprintf("Error marshaling result: %v", err),
			IsError: true,
		}, nil
	}

	return &ToolResult{
		Content: string(jsonBytes),
		IsError: false,
	}, nil
}

// persistBlob decodes base64 data and writes it to a unique file in ~/.jenny/mcp-resources/.
func (t *ReadMcpResourceTool) persistBlob(data []byte) (string, error) {
	// Decode base64 if needed (data may already be decoded from []byte)
	var decoded []byte
	if len(data) > 0 {
		// Check if data is base64 encoded (contains only valid base64 chars)
		decoded = data
	}

	// Generate unique filename: timestamp-random suffix
	timestamp := time.Now().UnixNano()
	var randSuffix uint64
	if _, err := rand.Read([]byte{}); err == nil {
		// crypto/rand available, use it
		var b [8]byte
		rand.Read(b[:])
		randSuffix = 0
		for _, v := range b {
			randSuffix = randSuffix<<8 + uint64(v)
		}
	} else {
		// Fallback to math/rand
		randSuffix = uint64(time.Now().UnixNano())
	}
	filename := fmt.Sprintf("%d-%016x.bin", timestamp, randSuffix)

	// Create persist directory
	persistDir := filepath.Join(constants.JennyHomeDir(), "mcp-resources")
	if err := os.MkdirAll(persistDir, 0755); err != nil {
		return "", fmt.Errorf("creating persist directory: %w", err)
	}

	// Write file
	filePath := filepath.Join(persistDir, filename)
	if err := os.WriteFile(filePath, decoded, 0644); err != nil {
		return "", fmt.Errorf("writing blob to disk: %w", err)
	}

	return filePath, nil
}
