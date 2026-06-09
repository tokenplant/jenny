package e2e_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/ipy/jenny/jenny_test/harness"
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
			// Check tool type - web_search uses WebSearchTool20250305Param format
			// which doesn't have input_schema, so skip it
			if toolType, ok := tool["type"].(string); ok && (toolType == "web_search" || toolType == "web_search_20250305") {
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

// TestMinimaxBadRequestReproduced was AC1: reproduce the MiniMax 400 error
// when tools have empty name or empty input_schema.
// NOTE: This test is removed because the fix is now in place in toolToSDK.
// AC2 (TestMinimaxToolSerializationPasses) is the regression test that validates
// the fix continues to work.
//
// To re-establish the baseline failure for future regression testing:
//1. Comment out the placeholder __arg__ injection in toolToSDK
// 2. Run this test - it should fail with 400 error
// 3. Restore the fix and confirm AC2 passes
// func TestMinimaxBadRequestReproduced(t *testing.T) { ... }

// TestMinimaxToolSerializationPasses is AC2: after fix, the same run succeeds
// with well-formed tool entries (non-empty name and non-empty schema).
func TestMinimaxToolSerializationPasses(t *testing.T) {
	mock := newMinimaxMockServer()
	t.Cleanup(mock.Close)

	env := []string{
		"ANTHROPIC_BASE_URL=" + mock.URL,
		"ANTHROPIC_AUTH_TOKEN=test-token",
		"ANTHROPIC_MODEL=MiniMax-M2.7",
	}

	res := harness.RunJenny(t, env, "--output-format", "stream-json", "-p", "run echo hello")
	if res.ExitCode != 0 {
		t.Fatalf("jenny exited %d; stderr=%q", res.ExitCode, res.Stderr)
	}

	// Verify the mock received well-formed tools
	reqs := mock.Requests()
	if len(reqs) == 0 {
		t.Fatal("no requests received by mock")
	}
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
