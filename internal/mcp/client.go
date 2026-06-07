// Package mcp provides MCP server configuration loading and management.
package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"strings"
	"sync"

	"github.com/ipy/jenny/internal/log"
	"github.com/ipy/jenny/internal/tool"
)

// Client represents an MCP client connection to a server.
type Client struct {
	Name string // Original server name
	cmd  string
	args []string
	env  map[string]string
	proc *proc
	mu   sync.Mutex
}

// proc holds the process handles for a subprocess transport.
type proc struct {
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	stdout  io.ReadCloser
	cleanFn func()
}

var (
	// clients is the registry of active MCP clients keyed by normalized server name.
	clients   = make(map[string]*Client)
	clientsMu sync.RWMutex

	// jsonID is used to generate unique JSON-RPC IDs.
	jsonID   int64
	jsonIDMu sync.Mutex
)

func nextJSONID() int64 {
	jsonIDMu.Lock()
	jsonID++
	id := jsonID
	jsonIDMu.Unlock()
	return id
}

// jsonRPCRequest represents a JSON-RPC request.
type jsonRPCRequest struct {
	JSONRPC string         `json:"jsonrpc"`
	ID      any            `json:"id"`
	Method  string         `json:"method"`
	Params  map[string]any `json:"params,omitempty"`
}

// jsonRPCResponse represents a JSON-RPC response.
type jsonRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *jsonRPCError   `json:"error,omitempty"`
}

// jsonRPCError represents a JSON-RPC error.
type jsonRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// initializeResult represents the result of an initialize call.
type initializeResult struct {
	ProtocolVersion string `json:"protocolVersion"`
	Capabilities    struct {
		Roots struct {
			Listen bool `json:"listen"`
		} `json:"roots"`
		Tools     any `json:"tools"`
		Resources any `json:"resources"`
	} `json:"capabilities"`
	ServerInfo struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	} `json:"serverInfo"`
}

// toolInfo represents a tool from the tools/list response.
type toolInfo struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

// toolsListResult represents the result of a tools/list call.
type toolsListResult struct {
	Tools []toolInfo `json:"tools"`
}

// toolsCallResult represents the result of a tools/call call.
type toolsCallResult struct {
	Content []contentPart `json:"content"`
}

// contentPart represents a content part in a tool result.
type contentPart struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
	Mime string `json:"mimeType,omitempty"`
}

// MCPTool implements tool.Tool for an MCP tool.
type MCPTool struct {
	serverName  string
	toolName    string
	inputSchema map[string]any
}

// Name returns the tool name with MCP prefix.
func (t *MCPTool) Name() string {
	return fmt.Sprintf("mcp__%s__%s", NormalizeName(t.serverName), NormalizeName(t.toolName))
}

// Description returns a description of the tool.
func (t *MCPTool) Description() string {
	return fmt.Sprintf("MCP tool %s from server %s", t.toolName, t.serverName)
}

// InputSchema returns the JSON schema for tool input.
func (t *MCPTool) InputSchema() map[string]any {
	return t.inputSchema
}

// Execute runs the tool with the given input and returns the result.
func (t *MCPTool) Execute(ctx context.Context, input map[string]any, cwd string) (*tool.ToolResult, error) {
	client := GetClient(t.serverName)
	if client == nil {
		return &tool.ToolResult{
			Content: fmt.Sprintf("Error: MCP server '%s' not found", t.serverName),
			IsError: true,
		}, nil
	}

	result, err := client.CallTool(t.toolName, input)
	if err != nil {
		return &tool.ToolResult{
			Content: fmt.Sprintf("Error calling MCP tool: %v", err),
			IsError: true,
		}, nil
	}

	return &tool.ToolResult{
		Content: result,
		IsError: false,
	}, nil
}

// NormalizeName normalizes a name for use in tool naming.
// Lowercase, non-alphanumeric characters become underscores, repeats collapsed.
func NormalizeName(name string) string {
	// Convert to lowercase
	result := strings.ToLower(name)

	// Replace non-alphanumeric chars with underscore
	var sb strings.Builder
	prevUnderscore := false
	for _, r := range result {
		if ('a' <= r && r <= 'z') || ('0' <= r && r <= '9') || r == '_' {
			if r == '_' {
				if !prevUnderscore {
					sb.WriteRune(r)
					prevUnderscore = true
				}
			} else {
				sb.WriteRune(r)
				prevUnderscore = false
			}
		} else {
			if !prevUnderscore {
				sb.WriteRune('_')
				prevUnderscore = true
			}
		}
	}

	// Trim leading/trailing underscores
	return strings.Trim(sb.String(), "_")
}

// NewClient creates a new MCP client for the given server.
func NewClient(name string, cmd string, args []string, env map[string]string) *Client {
	return &Client{
		Name: name,
		cmd:  cmd,
		args: args,
		env:  env,
	}
}

// Connect establishes a connection to the MCP server via stdio transport.
func (c *Client) Connect(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.proc != nil {
		return nil // Already connected
	}

	cmd := exec.CommandContext(ctx, c.cmd, c.args...)
	cmd.Stderr = os.Stderr

	// Set environment
	if c.env != nil {
		cmd.Env = os.Environ()
		for k, v := range c.env {
			cmd.Env = append(cmd.Env, k+"="+v)
		}
	}

	// Set up pipes
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("creating stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stdin.Close()
		return fmt.Errorf("creating stdout pipe: %w", err)
	}

	// Start the process
	if err := cmd.Start(); err != nil {
		stdin.Close()
		stdout.Close()
		return fmt.Errorf("starting MCP server process: %w", err)
	}

	c.proc = &proc{
		cmd:    cmd,
		stdin:  stdin,
		stdout: stdout,
		cleanFn: func() {
			cmd.Process.Kill()
			cmd.Wait()
		},
	}

	// Perform initialization
	if err := c.initialize(ctx); err != nil {
		c.cleanup()
		return err
	}

	return nil
}

// initialize performs the MCP handshake.
func (c *Client) initialize(ctx context.Context) error {
	// Send initialize request
	req := jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      nextJSONID(),
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

	resp, err := c.sendRequest(ctx, req)
	if err != nil {
		return fmt.Errorf("initialize request failed: %w", err)
	}

	if resp.Error != nil {
		return fmt.Errorf("initialize error: %s (code %d)", resp.Error.Message, resp.Error.Code)
	}

	// Parse the result to get server capabilities
	var initResult initializeResult
	if err := json.Unmarshal(resp.Result, &initResult); err != nil {
		return fmt.Errorf("parsing initialize result: %w", err)
	}

	log.Debug("MCP server initialized",
		"server", c.Name,
		"protocolVersion", initResult.ProtocolVersion,
		"serverInfo", initResult.ServerInfo.Name+"@"+initResult.ServerInfo.Version,
	)

	// Send notifications/initialized (notification, no response expected)
	notif := jsonRPCRequest{
		JSONRPC: "2.0",
		Method:  "notifications/initialized",
	}
	_ = c.sendNotification(notif)

	return nil
}

// ListTools discovers tools from the MCP server.
func (c *Client) ListTools(ctx context.Context) ([]MCPTool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.proc == nil {
		return nil, fmt.Errorf("not connected")
	}

	req := jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      nextJSONID(),
		Method:  "tools/list",
	}

	resp, err := c.sendRequest(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("tools/list request failed: %w", err)
	}

	if resp.Error != nil {
		return nil, fmt.Errorf("tools/list error: %s (code %d)", resp.Error.Message, resp.Error.Code)
	}

	var listResult toolsListResult
	if err := json.Unmarshal(resp.Result, &listResult); err != nil {
		return nil, fmt.Errorf("parsing tools/list result: %w", err)
	}

	tools := make([]MCPTool, 0, len(listResult.Tools))
	for _, t := range listResult.Tools {
		var inputSchema map[string]any
		if err := json.Unmarshal(t.InputSchema, &inputSchema); err != nil {
			log.Warn("failed to parse tool input schema", "tool", t.Name, "error", err)
			inputSchema = map[string]any{"type": "object"}
		}
		tools = append(tools, MCPTool{
			serverName:  c.Name,
			toolName:    t.Name,
			inputSchema: inputSchema,
		})
		log.Debug("discovered MCP tool", "server", c.Name, "tool", t.Name)
	}

	return tools, nil
}

// CallTool calls a tool on the MCP server.
func (c *Client) CallTool(name string, arguments map[string]any) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.proc == nil {
		return "", fmt.Errorf("not connected to MCP server %s", c.Name)
	}

	ctx := context.Background()
	req := jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      nextJSONID(),
		Method:  "tools/call",
		Params: map[string]any{
			"name":      name,
			"arguments": arguments,
		},
	}

	resp, err := c.sendRequest(ctx, req)
	if err != nil {
		return "", fmt.Errorf("tools/call request failed: %w", err)
	}

	if resp.Error != nil {
		return "", fmt.Errorf("tools/call error: %s (code %d)", resp.Error.Message, resp.Error.Code)
	}

	var callResult toolsCallResult
	if err := json.Unmarshal(resp.Result, &callResult); err != nil {
		return "", fmt.Errorf("parsing tools/call result: %w", err)
	}

	// Extract text content from result
	var result strings.Builder
	for _, part := range callResult.Content {
		if part.Type == "text" {
			result.WriteString(part.Text)
		} else {
			fmt.Fprintf(&result, "[%s: %s]", part.Type, part.Text)
		}
	}

	return result.String(), nil
}

// sendRequest sends a JSON-RPC request and waits for a response.
// Caller must hold c.mu.
func (c *Client) sendRequest(_ context.Context, req jsonRPCRequest) (*jsonRPCResponse, error) {
	if c.proc == nil || c.proc.stdin == nil || c.proc.stdout == nil {
		return nil, fmt.Errorf("not connected")
	}

	// Marshal the request
	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshaling request: %w", err)
	}

	// Send the request
	if _, err := c.proc.stdin.Write(append(data, '\n')); err != nil {
		return nil, fmt.Errorf("writing request: %w", err)
	}

	// Read the response
	reader := bufio.NewReader(c.proc.stdout)
	line, err := reader.ReadBytes('\n')
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	var resp jsonRPCResponse
	if err := json.Unmarshal(line, &resp); err != nil {
		return nil, fmt.Errorf("parsing response: %w", err)
	}

	return &resp, nil
}

// sendNotification sends a JSON-RPC notification (no response expected).
// Caller must hold c.mu.
func (c *Client) sendNotification(notif jsonRPCRequest) error {
	if c.proc == nil || c.proc.stdin == nil {
		return fmt.Errorf("not connected")
	}

	data, err := json.Marshal(notif)
	if err != nil {
		return fmt.Errorf("marshaling notification: %w", err)
	}

	_, err = c.proc.stdin.Write(append(data, '\n'))
	return err
}

// cleanup shuts down the process and cleans up resources.
func (c *Client) cleanup() {
	if c.proc == nil {
		return
	}

	// Send shutdown notification if possible
	if c.proc.stdin != nil {
		notif := jsonRPCRequest{
			JSONRPC: "2.0",
			Method:  "notifications/shutdown",
		}
		data, _ := json.Marshal(notif)
		c.proc.stdin.Write(append(data, '\n'))
		c.proc.stdin.Close()
	}

	if c.proc.cleanFn != nil {
		c.proc.cleanFn()
	}

	c.proc = nil
}

// Disconnect disconnects from the MCP server.
func (c *Client) Disconnect() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cleanup()
}

// GetClient returns the client for a given normalized server name.
func GetClient(serverName string) *Client {
	clientsMu.RLock()
	defer clientsMu.RUnlock()
	return clients[NormalizeName(serverName)]
}

// ConnectAll connects to all MCP servers in the configuration.
func ConnectAll(cfg map[string]MCPServerDef) error {
	clientsMu.Lock()
	defer clientsMu.Unlock()

	for name, def := range cfg {
		if def.Command == "" {
			continue // Skip non-stdio servers for now
		}

		client := NewClient(name, def.Command, def.Args, def.Env)
		clients[NormalizeName(name)] = client

		ctx := context.Background()
		if err := client.Connect(ctx); err != nil {
			return fmt.Errorf("connecting to MCP server %q: %w", name, err)
		}
	}

	return nil
}

// ShutdownAll disconnects all MCP clients.
func ShutdownAll() {
	clientsMu.Lock()
	defer clientsMu.Unlock()

	for _, client := range clients {
		client.Disconnect()
	}
	clients = make(map[string]*Client)
}

// GetTools returns all discovered MCP tools from all connected servers.
func GetTools() []any {
	clientsMu.RLock()
	defer clientsMu.RUnlock()

	var allTools []any
	for _, client := range clients {
		ctx := context.Background()
		tools, err := client.ListTools(ctx)
		if err != nil {
			log.Warn("failed to list tools", "server", client.Name, "error", err)
			continue
		}
		for i := range tools {
			allTools = append(allTools, &tools[i])
		}
	}

	return allTools
}

// GetMCPClients returns a copy of the map of normalized server names to clients.
func GetMCPClients() map[string]*Client {
	clientsMu.RLock()
	defer clientsMu.RUnlock()

	result := make(map[string]*Client, len(clients))
	maps.Copy(result, clients)
	return result
}
