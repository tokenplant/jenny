package e2e_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/ipy/jenny/parity/harness"
)

// minimaxMockServer is a mock that returns MiniMax-style 400 errors when
// tools have empty name or empty input_schema.
type minimaxMockServer struct {
	*httptest.Server
	mu       sync.Mutex
	requests []map[string]any
}

func newMinimaxMockServer() *minimaxMockServer {
	m := &minimaxMockServer{}
	m.Server = httptest.NewServer(http.HandlerFunc(m.handle))
	return m
}

func (m *minimaxMockServer) handle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "read body: "+err.Error(), http.StatusBadRequest)
		return
	}

	var decoded map[string]any
	if err := json.Unmarshal(body, &decoded); err != nil {
		http.Error(w, "decode body: "+err.Error(), http.StatusBadRequest)
		return
	}

	m.mu.Lock()
	m.requests = append(m.requests, decoded)
	m.mu.Unlock()

	// Check tools array for empty name or empty parameters
	if tools, ok := decoded["tools"].([]any); ok {
		for _, toolEntry := range tools {
			tool, ok := toolEntry.(map[string]any)
			if !ok {
				continue
			}
			// Check for empty name
			name, ok := tool["name"].(string)
			if !ok || name == "" {
				m.writeMinimaxError(w, "function name or parameters is empty (2013)")
				return
			}
			// Check for empty/missing input_schema
			inputSchema, ok := tool["input_schema"].(map[string]any)
			if !ok || inputSchema == nil {
				m.writeMinimaxError(w, "function name or parameters is empty (2013)")
				return
			}
			// Check if properties is missing or empty object
			props, ok := inputSchema["properties"]
			if !ok || props == nil {
				m.writeMinimaxError(w, "function name or parameters is empty (2013)")
				return
			}
			propsMap, ok := props.(map[string]any)
			if !ok || len(propsMap) == 0 {
				// For MiniMax, an empty properties object is considered "empty parameters"
				m.writeMinimaxError(w, "function name or parameters is empty (2013)")
				return
			}
		}
	} else {
		// No tools array at all - MiniMax considers this an error
		m.writeMinimaxError(w, "function name or parameters is empty (2013)")
		return
	}

	// No issues found - serve the tool-use SSE response inline
	// First request returns tool_use, second returns final text
	m.mu.Lock()
	reqCount := len(m.requests)
	m.mu.Unlock()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
	if reqCount == 1 {
		// Turn 1: assistant returns tool_use for Bash
		_, _ = w.Write([]byte("event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_tu1\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"MiniMax-M2.7\",\"content\":[],\"stop_reason\":null,\"usage\":{\"input_tokens\":150,\"output_tokens\":0}}}\n\n"))
		_, _ = w.Write([]byte("event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"tool_use\",\"id\":\"toolu_bash01\",\"name\":\"Bash\",\"input\":{}}}\n\n"))
		_, _ = w.Write([]byte("event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":\"{\\\"command\\\":\\\"echo hello\\\"}\"}}\n\n"))
		_, _ = w.Write([]byte("event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n"))
		_, _ = w.Write([]byte("event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"tool_use\",\"stop_sequence\":null},\"usage\":{\"output_tokens\":20}}\n\n"))
		_, _ = w.Write([]byte("event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"))
	} else {
		// Turn 2: assistant returns final text
		_, _ = w.Write([]byte("event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_02\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"MiniMax-M2.7\",\"content\":[],\"stop_reason\":null,\"usage\":{\"input_tokens\":200,\"output_tokens\":0}}}\n\n"))
		_, _ = w.Write([]byte("event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n"))
		_, _ = w.Write([]byte("event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"Hello from MiniMax mock.\"}}\n\n"))
		_, _ = w.Write([]byte("event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n"))
		_, _ = w.Write([]byte("event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\",\"stop_sequence\":null},\"usage\":{\"output_tokens\":5}}\n\n"))
		_, _ = w.Write([]byte("event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"))
	}
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (m *minimaxMockServer) writeMinimaxError(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"type": "error",
		"error": map[string]any{
			"type":    "invalid_request_error",
			"message": msg,
		},
	})
}

func (m *minimaxMockServer) Requests() []map[string]any {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]map[string]any, len(m.requests))
	copy(out, m.requests)
	return out
}

// TestMinimaxBadRequestReproduced is AC1: verify that with the fix in place,
// jenny does NOT trigger MiniMax error 2013 even when tools would have empty
// schemas. The fix in toolToSDK adds a placeholder __arg__ property, so the
// mock (which rejects empty schemas) never returns 400.
// If someone removes the fix, this test will fail because the mock will
// start rejecting the empty schemas.
func TestMinimaxBadRequestReproduced(t *testing.T) {
	mock := newMinimaxMockServer()
	t.Cleanup(mock.Close)

	// AC4: Use a MiniMax-like URL to trigger provider detection.
	// Append /minimaxi to the mock URL so providerFromBaseURL
	// returns "minimax" and __arg__ is added, while the host
	// part (127.0.0.1:PORT) resolves correctly via DNS.
	minimaxURL := mock.URL + "/minimaxi"

	env := []string{
		"ANTHROPIC_BASE_URL=" + minimaxURL,
		"ANTHROPIC_AUTH_TOKEN=test-token",
		"ANTHROPIC_MODEL=MiniMax-M2.7",
	}

	// Run with a prompt that triggers a tool call
	res := harness.RunJenny(t, env, "--output-format", "stream-json", "-p", "run echo hello")

	// After the fix: exit should be 0 (no 400 error from mock)
	if res.ExitCode != 0 {
		t.Fatalf("jenny exited %d; stderr=%q", res.ExitCode, res.Stderr)
	}

	// Verify no 400 errors were received - the mock should have accepted all requests
	reqs := mock.Requests()
	if len(reqs) == 0 {
		t.Fatal("no requests received by mock")
	}
}

// TestMinimaxToolSerializationPasses is AC2+AC3: after fix, the same run succeeds
// with well-formed tool entries (non-empty name and non-empty schema), the Bash
// tool has the correct shape (command.type == "string"), and the tool_result
// round-trip completes with exit 0.
func TestMinimaxToolSerializationPasses(t *testing.T) {
	mock := newMinimaxMockServer()
	t.Cleanup(mock.Close)

	// AC4: Use a MiniMax-like URL to trigger provider detection.
	// Append /minimaxi to the mock URL so providerFromBaseURL
	// returns "minimax" and __arg__ is added, while the host
	// part (127.0.0.1:PORT) resolves correctly via DNS.
	minimaxURL := mock.URL + "/minimaxi"

	env := []string{
		"ANTHROPIC_BASE_URL=" + minimaxURL,
		"ANTHROPIC_AUTH_TOKEN=test-token",
		"ANTHROPIC_MODEL=MiniMax-M2.7",
	}

	res := harness.RunJenny(t, env, "--output-format", "stream-json", "-p", "run echo hello")
	// AC2/AC3: exit 0 means no 400 error and successful tool_result round-trip
	if res.ExitCode != 0 {
		t.Fatalf("jenny exited %d; stderr=%q", res.ExitCode, res.Stderr)
	}

	reqs := mock.Requests()
	if len(reqs) == 0 {
		t.Fatal("no requests received by mock")
	}

	// AC3: verify the tool_result round-trip - need at least 2 requests
	// (first with tools, second with tool_result)
	if len(reqs) < 2 {
		t.Fatalf("AC3: expected at least 2 requests (tools + tool_result), got %d", len(reqs))
	}

	// AC3: verify first request has Bash tool with correct shape
	firstReq := reqs[0]
	tools, ok := firstReq["tools"].([]any)
	if !ok {
		t.Fatal("first request: tools is not an array")
	}
	var bashTool map[string]any
	for _, toolEntry := range tools {
		tool, ok := toolEntry.(map[string]any)
		if !ok {
			continue
		}
		// Skip web_search tools
		if toolType, ok := tool["type"].(string); ok && (toolType == "web_search" || toolType == "web_search_20250305") {
			continue
		}
		name, _ := tool["name"].(string)
		if name == "Bash" {
			bashTool = tool
			break
		}
	}
	if bashTool == nil {
		t.Fatal("AC3: no Bash tool found in first request")
	}
	// AC3: verify Bash tool has input_schema.properties.command.type == "string"
	inputSchema, ok := bashTool["input_schema"].(map[string]any)
	if !ok {
		t.Fatal("AC3: Bash tool input_schema is not a map")
	}
	props, ok := inputSchema["properties"].(map[string]any)
	if !ok {
		t.Fatal("AC3: input_schema.properties is not a map")
	}
	commandProp, ok := props["command"].(map[string]any)
	if !ok {
		t.Fatal("AC3: input_schema.properties.command is missing or not a map")
	}
	if commandType, _ := commandProp["type"].(string); commandType != "string" {
		t.Errorf("AC3: input_schema.properties.command.type = %q; want \"string\"", commandType)
	}

	// AC2: verify all requests have well-formed tools (non-empty name and non-empty schema)
	for i, req := range reqs {
		tools, ok := req["tools"].([]any)
		if !ok {
			t.Fatalf("request %d: tools is not an array", i)
		}
		for j, toolEntry := range tools {
			tool, ok := toolEntry.(map[string]any)
			if !ok {
				t.Fatalf("tool %d: not a map", j)
			}
			// Skip web_search tools which use WebSearchTool20250305Param format
			if toolType, ok := tool["type"].(string); ok && (toolType == "web_search" || toolType == "web_search_20250305") {
				continue
			}
			name, _ := tool["name"].(string)
			if name == "" {
				t.Errorf("tool %d: name is empty", j)
			}
			inputSchema, ok := tool["input_schema"].(map[string]any)
			if !ok || inputSchema == nil {
				t.Errorf("tool %d: input_schema is nil or not a map", j)
				continue
			}
			props, ok := inputSchema["properties"].(map[string]any)
			if !ok || len(props) == 0 {
				t.Errorf("tool %d: input_schema.properties is empty or missing", j)
			}
		}
	}
}

// TestAnthropicEndpointRegression is AC3: verify that when ANTHROPIC_BASE_URL
// does NOT contain "minimaxi", the __arg__ placeholder is NOT added to tools.
// This is a regression guard ensuring the Anthropic endpoint is not affected
// by the MiniMax compatibility fix.
func TestAnthropicEndpointRegression(t *testing.T) {
	mock := harness.NewMockServer(cassettesDir)
	t.Cleanup(mock.Close)

	// Use a non-MiniMax URL (no "minimaxi" in it) so provider detection
	// returns "anthropic" and __arg__ is NOT added.
	env := []string{
		"ANTHROPIC_BASE_URL=" + mock.URL() + "/cassette/" + echoHelloCassette,
		"ANTHROPIC_AUTH_TOKEN=test-token",
		"ANTHROPIC_MODEL=",
	}

	res := harness.RunJenny(t, env, "--output-format", "stream-json", "-p", "echo hello")

	// AC3: exit 0 means successful completion
	if res.ExitCode != 0 {
		t.Fatalf("jenny exited %d; stderr=%q", res.ExitCode, res.Stderr)
	}

	// AC3: verify no __arg__ appears in any request's tools array
	reqs := mock.Requests()
	for i, req := range reqs {
		tools, ok := req.Body["tools"].([]any)
		if !ok {
			continue // no tools in this request is fine
		}
		for j, toolEntry := range tools {
			tool, ok := toolEntry.(map[string]any)
			if !ok {
				continue
			}
			// Skip web_search tools
			if toolType, ok := tool["type"].(string); ok && (toolType == "web_search" || toolType == "web_search_20250305") {
				continue
			}
			inputSchema, ok := tool["input_schema"].(map[string]any)
			if !ok {
				continue
			}
			props, ok := inputSchema["properties"].(map[string]any)
			if !ok {
				t.Errorf("request %d tool %d: properties is not a map", i, j)
				continue
			}
			if _, hasArg := props["__arg__"]; hasArg {
				t.Errorf("request %d tool %d: found __arg__ in properties; should not be present for non-MiniMax provider", i, j)
			}
		}
	}
}
