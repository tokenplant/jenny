// Fake MCP server for integration testing.
// This simple program implements the MCP protocol over stdio.
// Build with: go build -o fake-mcp-server ./testdata/fake-mcp-server
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

type jsonRPCRequest struct {
	JSONRPC string                 `json:"jsonrpc"`
	ID      any `json:"id"`
	Method  string                 `json:"method"`
	Params map[string]interface{} `json:"params,omitempty"`
}

type jsonRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any              `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *jsonRPCError   `json:"error,omitempty"`
}

type jsonRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func main() {
	reader := bufio.NewReader(os.Stdin)
	writer := bufio.NewWriter(os.Stdout)

	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			return
		}

		lineStr := strings.TrimSpace(string(line))
		if lineStr == "" {
			continue
		}

		var req jsonRPCRequest
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			return
		}

		resp := jsonRPCResponse{JSONRPC: "2.0", ID: req.ID}

		switch req.Method {
		case "initialize":
			resp.Result = json.RawMessage(`{"protocolVersion":"2025-03-26","capabilities":{},"serverInfo":{"name":"fake-mcp-server","version":"1.0.0"}}`)

		case "tools/list":
			resp.Result = json.RawMessage(`{"tools":[{"name":"test-tool","description":"A test tool","inputSchema":{"type":"object"}},{"name":"echo","description":"Echo back the input","inputSchema":{"type":"object","properties":{"text":{"type":"string"}}}}]}`)

		case "tools/call":
			name, _ := req.Params["name"].(string)
			args, _ := req.Params["arguments"].(map[string]interface{})
			if name == "echo" {
				text, _ := args["text"].(string)
				resp.Result = json.RawMessage(fmt.Sprintf(`{"content":[{"type":"text","text":"echo: %s"}]}`, text))
			} else {
				resp.Result = json.RawMessage(fmt.Sprintf(`{"content":[{"type":"text","text":"result from %s"}]}`, name))
			}

		case "notifications/initialized", "notifications/shutdown":
			// Notifications have no response
			continue

		default:
			resp.Error = &jsonRPCError{Code: -32601, Message: "method not found"}
		}

		data, _ := json.Marshal(resp)
		writer.Write(data)
		writer.WriteByte('\n')
		writer.Flush()
	}
}